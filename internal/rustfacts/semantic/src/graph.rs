use ra_ap_hir::{ModuleDef, PathResolution, Semantics};
use ra_ap_ide_db::RootDatabase;
use ra_ap_syntax::{AstNode, SyntaxNode, ast};
use ra_ap_vfs::{FileId, Vfs};
use serde::Serialize;

#[derive(Serialize)]
pub(crate) struct Graph {
    protocol: &'static str,
    edges: Vec<Edge>,
    problems: Vec<Problem>,
}

#[derive(Serialize, PartialEq, Eq, PartialOrd, Ord)]
struct Edge {
    source: Location,
    target: Location,
    expanded: bool,
}

#[derive(Serialize, PartialEq, Eq, PartialOrd, Ord)]
struct Location {
    path: String,
    start: u32,
    end: u32,
}

#[derive(Serialize)]
struct Problem {
    location: Location,
    message: String,
}

pub(crate) fn collect(
    database: &RootDatabase,
    vfs: &Vfs,
    files: &[String],
) -> Result<Graph, Box<dyn std::error::Error>> {
    let semantics = Semantics::new(database);
    let mut graph = Graph {
        protocol: "rust-semantic/v1",
        edges: Vec::new(),
        problems: Vec::new(),
    };
    let mut remaining = 1_000_000;
    for path in files {
        let (file, _) = vfs
            .iter()
            .find(|(_, candidate)| candidate.to_string() == *path)
            .ok_or_else(|| {
                format!("selected file is absent from the semantic workspace: {path}")
            })?;
        let module = semantics
            .file_to_module_def(file)
            .ok_or_else(|| format!("selected file has no compiled module: {path}"))?;
        let edition = module
            .as_source_file_id(database)
            .ok_or("selected file has no source-file identity")?;
        let root = semantics.parse(edition);
        graph.walk(&semantics, vfs, root.syntax(), 0, &mut remaining)?;
    }
    graph.edges.sort();
    graph.edges.dedup();
    Ok(graph)
}

impl Graph {
    fn walk(
        &mut self,
        semantics: &Semantics<'_, RootDatabase>,
        vfs: &Vfs,
        node: &SyntaxNode,
        depth: usize,
        remaining: &mut usize,
    ) -> Result<(), Box<dyn std::error::Error>> {
        if depth > 128 || *remaining == 0 {
            return Err("semantic expansion exceeds its traversal limit".into());
        }
        *remaining -= 1;
        if let Some(call) = ast::MacroCall::cast(node.clone()) {
            return self.macro_call(semantics, vfs, &call, depth, remaining);
        }
        if let Some(path) = ast::Path::cast(node.clone()) {
            self.path(semantics, vfs, &path, depth > 0);
        }
        if let Some(call) = ast::MethodCallExpr::cast(node.clone()) {
            match semantics.resolve_method_call(&call) {
                Some(function) => self.reference(
                    semantics,
                    vfs,
                    node,
                    ModuleDef::Function(function),
                    depth > 0,
                ),
                None => self.problem(semantics, vfs, node, "method resolution is incomplete"),
            }
        }
        for child in node.children() {
            self.walk(semantics, vfs, &child, depth, remaining)?;
        }
        Ok(())
    }

    fn macro_call(
        &mut self,
        semantics: &Semantics<'_, RootDatabase>,
        vfs: &Vfs,
        call: &ast::MacroCall,
        depth: usize,
        remaining: &mut usize,
    ) -> Result<(), Box<dyn std::error::Error>> {
        let Some(id) = semantics.to_def(call) else {
            self.problem(
                semantics,
                vfs,
                call.syntax(),
                "macro resolution is incomplete",
            );
            return Ok(());
        };
        if let Some(definition) = semantics.resolve_macro_call(call) {
            self.reference(
                semantics,
                vfs,
                call.syntax(),
                ModuleDef::Macro(definition),
                depth > 0,
            );
        }
        let expansion = semantics.expand(id);
        if let Some(error) = expansion.err {
            self.problem(
                semantics,
                vfs,
                call.syntax(),
                &format!("macro expansion failed: {error:?}"),
            );
            return Ok(());
        }
        self.walk(semantics, vfs, &expansion.value, depth + 1, remaining)
    }

    fn path(
        &mut self,
        semantics: &Semantics<'_, RootDatabase>,
        vfs: &Vfs,
        path: &ast::Path,
        expanded: bool,
    ) {
        if path
            .syntax()
            .parent()
            .is_some_and(|node| ast::Path::can_cast(node.kind()))
            || path
                .syntax()
                .ancestors()
                .any(|node| ast::Attr::can_cast(node.kind()))
        {
            return;
        }
        match semantics.resolve_path(path) {
            Some(PathResolution::Def(definition)) => {
                self.reference(semantics, vfs, path.syntax(), definition, expanded)
            }
            Some(_) => {}
            None => self.problem(
                semantics,
                vfs,
                path.syntax(),
                "path resolution is incomplete",
            ),
        }
    }

    fn reference(
        &mut self,
        semantics: &Semantics<'_, RootDatabase>,
        vfs: &Vfs,
        node: &SyntaxNode,
        definition: ModuleDef,
        expanded: bool,
    ) {
        let target = match definition {
            ModuleDef::Module(module) => Some(module),
            other => other.module(semantics.db),
        };
        let Some(module) = target else {
            return;
        };
        let source = location(semantics, vfs, node);
        let module_source = module.definition_source(semantics.db);
        let target_node = semantics.parse_or_expand(module_source.file_id);
        let target = location(semantics, vfs, &target_node);
        if source.path != target.path {
            self.edges.push(Edge {
                source,
                target,
                expanded,
            });
        }
    }

    fn problem(
        &mut self,
        semantics: &Semantics<'_, RootDatabase>,
        vfs: &Vfs,
        node: &SyntaxNode,
        message: &str,
    ) {
        self.problems.push(Problem {
            location: location(semantics, vfs, node),
            message: message.into(),
        });
    }
}

fn location(semantics: &Semantics<'_, RootDatabase>, vfs: &Vfs, node: &SyntaxNode) -> Location {
    let range = semantics.original_range(node);
    Location {
        path: path(vfs, range.file_id),
        start: range.range.start().into(),
        end: range.range.end().into(),
    }
}

fn path(vfs: &Vfs, file: FileId) -> String {
    vfs.file_path(file).to_string()
}
