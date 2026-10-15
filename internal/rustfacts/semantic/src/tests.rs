use crate::{Request, workspace};
use serde_json::Value;
use std::{
    fs,
    path::PathBuf,
    time::{SystemTime, UNIX_EPOCH},
};

struct Fixture {
    root: PathBuf,
}

impl Fixture {
    fn new() -> Self {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        let root = std::env::temp_dir().join(format!(
            "code-polishy-semantic-{}-{nonce}",
            std::process::id()
        ));
        fs::create_dir(&root).unwrap();
        let fixture = Self { root };
        fixture.write(
            "Cargo.toml",
            "[package]\nname='semantic-fixture'\nversion='1.0.0'\nedition='2024'\n",
        );
        fixture.write(
            "Cargo.lock",
            "version=4\n[[package]]\nname='semantic-fixture'\nversion='1.0.0'\n",
        );
        fixture
    }

    fn write(&self, path: &str, source: &str) {
        let path = self.root.join(path);
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        fs::write(path, source).unwrap();
    }

    fn request(&self, files: &[&str]) -> Request {
        let compiler = PathBuf::from(std::env::var("RUSTC").expect("pinned RUSTC is required"));
        let sdk = compiler.parent().unwrap().parent().unwrap();
        Request {
            protocol: "rust-semantic/v1".into(),
            manifest: self.root.join("Cargo.toml").to_str().unwrap().into(),
            target: std::env::var("CODE_POLISHY_RUST_HOST")
                .expect("governed native host is required"),
            cargo: std::env::var("CARGO").expect("pinned CARGO is required"),
            proc_macro_server: sdk
                .join("libexec")
                .join(format!(
                    "rust-analyzer-proc-macro-srv{}",
                    std::env::consts::EXE_SUFFIX
                ))
                .to_str()
                .unwrap()
                .into(),
            files: files
                .iter()
                .map(|path| self.root.join(path).to_str().unwrap().into())
                .collect(),
            packages: Vec::new(),
            features: Vec::new(),
            no_default_features: false,
        }
    }

    fn graph(&self, files: &[&str]) -> Value {
        serde_json::to_value(workspace::analyze(&self.request(files)).unwrap()).unwrap()
    }
}

impl Drop for Fixture {
    fn drop(&mut self) {
        fs::remove_dir_all(&self.root).unwrap();
    }
}

#[test]
fn generated_references_retain_dollar_crate_hygiene_and_call_site() {
    let fixture = Fixture::new();
    fixture.write(
        "src/lib.rs",
        "#![no_std]\n#[macro_use] mod macros;\nmod consumer;\nmod lower;\npub use consumer::run;\n",
    );
    fixture.write(
        "src/macros.rs",
        "macro_rules! hidden_call { () => { $crate::lower::value() }; }\n",
    );
    fixture.write("src/lower.rs", "pub fn value() -> u8 { 7 }\n");
    let source =
        "mod lower { pub fn value() -> u8 { 9 } }\npub fn run() -> u8 { hidden_call!() }\n";
    fixture.write("src/consumer.rs", source);
    let graph = fixture.graph(&["src/consumer.rs"]);
    assert_eq!(graph["problems"], serde_json::json!([]));
    let target = fixture
        .root
        .join("src/lower.rs")
        .to_str()
        .unwrap()
        .to_owned();
    let edge = graph["edges"]
        .as_array()
        .unwrap()
        .iter()
        .find(|edge| edge["target"]["path"] == target)
        .expect("generated dependency was omitted");
    assert_eq!(edge["expanded"], true);
    assert_eq!(
        edge["source"]["start"],
        source.find("hidden_call!").unwrap()
    );
    assert_eq!(
        edge["source"]["path"],
        fixture.root.join("src/consumer.rs").to_str().unwrap()
    );
}

#[test]
fn nested_expansion_keeps_dependencies_introduced_without_input_tokens() {
    let fixture = Fixture::new();
    fixture.write(
        "src/lib.rs",
        "#![no_std]\n#[macro_use] mod macros;\nmod lower;\nmod consumer;\npub use consumer::run;\n",
    );
    fixture.write("src/macros.rs", "macro_rules! inner { () => { $crate::lower::value() }; }\nmacro_rules! outer { () => { inner!() }; }\n");
    fixture.write("src/lower.rs", "pub fn value() -> u8 { 7 }\n");
    fixture.write("src/consumer.rs", "pub fn run() -> u8 { outer!() }\n");
    let graph = fixture.graph(&["src/consumer.rs"]);
    assert_eq!(graph["problems"], serde_json::json!([]));
    assert!(
        graph["edges"]
            .as_array()
            .unwrap()
            .iter()
            .any(|edge| edge["expanded"] == true
                && edge["target"]["path"] == fixture.root.join("src/lower.rs").to_str().unwrap())
    );
}

#[test]
fn invalid_macro_expansion_is_a_failed_compilation_not_an_empty_graph() {
    let fixture = Fixture::new();
    fixture.write("src/lib.rs", "#![no_std]\nmacro_rules! broken { () => { missing::value() }; }\npub fn run() { broken!(); }\n");
    let result = workspace::analyze(&fixture.request(&["src/lib.rs"]));
    let message = result
        .err()
        .expect("invalid expansion was accepted")
        .to_string();
    assert!(message.contains("Cargo compilation failed"), "{message}");
}
