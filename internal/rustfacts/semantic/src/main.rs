mod graph;
mod workspace;

#[cfg(test)]
mod tests;

use serde::Deserialize;
use std::io::{self, Read};

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Request {
    protocol: String,
    manifest: String,
    target: String,
    cargo: String,
    proc_macro_server: String,
    files: Vec<String>,
    #[serde(default)]
    packages: Vec<String>,
    #[serde(default)]
    features: Vec<String>,
    #[serde(default)]
    no_default_features: bool,
}

fn main() {
    if let Err(error) = run() {
        eprintln!("{error}");
        std::process::exit(1);
    }
}

fn run() -> Result<(), Box<dyn std::error::Error>> {
    let mut input = Vec::new();
    io::stdin()
        .take(8 * 1024 * 1024 + 1)
        .read_to_end(&mut input)?;
    if input.len() > 8 * 1024 * 1024 {
        return Err("semantic request exceeds 8 MiB".into());
    }
    let request: Request = serde_json::from_slice(&input)?;
    if request.protocol != "rust-semantic/v1"
        || request.files.is_empty()
        || request.files.len() > 4096
    {
        return Err("unsupported semantic request protocol or file inventory".into());
    }
    let result = workspace::analyze(&request)?;
    serde_json::to_writer(io::stdout().lock(), &result)?;
    Ok(())
}
