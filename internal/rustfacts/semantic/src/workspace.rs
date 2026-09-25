use crate::{Request, graph};
use ra_ap_load_cargo::{LoadCargoConfig, ProcMacroServerChoice, load_workspace};
use ra_ap_project_model::{
    CargoConfig, CargoFeatures, ProjectManifest, ProjectWorkspace, ProjectWorkspaceKind,
};
use ra_ap_vfs::AbsPathBuf;

pub(crate) fn analyze(request: &Request) -> Result<graph::Graph, Box<dyn std::error::Error>> {
    let manifest = ProjectManifest::from_manifest_file(absolute(&request.manifest)?)?;
    let config = configuration(request);
    let mut workspace =
        ProjectWorkspace::load(manifest, &config, &|message| eprintln!("{message}"))?;
    match &workspace.kind {
        ProjectWorkspaceKind::Cargo { error: None, .. } => {}
        _ => return Err("complete Cargo metadata is required".into()),
    }
    if workspace.target.is_err() || workspace.toolchain.is_none() {
        return Err("compiler target or toolchain facts are unavailable".into());
    }
    let scripts = workspace.run_build_scripts(&config, &|message| eprintln!("{message}"))?;
    if let Some(error) = scripts.error() {
        return Err(format!("Cargo compilation failed: {error}").into());
    }
    workspace.set_build_scripts(scripts);
    let load = LoadCargoConfig {
        load_out_dirs_from_check: true,
        with_proc_macro_server: ProcMacroServerChoice::Explicit(absolute(
            &request.proc_macro_server,
        )?),
        prefill_caches: false,
        num_worker_threads: 1,
        proc_macro_processes: 1,
    };
    let (database, vfs, server) = load_workspace(workspace, &config.extra_env, &load)?;
    if server.is_none() {
        return Err("the pinned procedural macro server is unavailable".into());
    }
    graph::collect(&database, &vfs, &request.files)
}

fn configuration(request: &Request) -> CargoConfig {
    let mut command = vec![
        request.cargo.clone(),
        "check".into(),
        "--frozen".into(),
        "--manifest-path".into(),
        request.manifest.clone(),
        "--message-format=json".into(),
        "--target".into(),
        request.target.clone(),
    ];
    for package in &request.packages {
        command.extend(["--package".into(), package.clone()]);
    }
    for feature in &request.features {
        command.extend(["--features".into(), feature.clone()]);
    }
    if request.no_default_features {
        command.push("--no-default-features".into());
    }
    CargoConfig {
        target: Some(request.target.clone()),
        features: CargoFeatures::Selected {
            features: request.features.clone(),
            no_default_features: request.no_default_features,
        },
        extra_args: vec!["--frozen".into()],
        run_build_script_command: Some(command),
        wrap_rustc_in_build_scripts: false,
        set_test: false,
        ..CargoConfig::default()
    }
}

fn absolute(path: &str) -> Result<AbsPathBuf, Box<dyn std::error::Error>> {
    Ok(AbsPathBuf::try_from(path.to_owned()).map_err(|_| "an absolute UTF-8 path is required")?)
}
