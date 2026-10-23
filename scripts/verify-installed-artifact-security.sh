#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 2 || "$1" != /* || "$2" != /* ]]; then
  echo "usage: verify-installed-artifact-security.sh ABSOLUTE_INSTALLATION ABSOLUTE_FIXTURE" >&2
  exit 2
fi

python3 - "$1" "$2" <<'PY'
import json
import sys
import pathlib
import subprocess

installation = pathlib.Path(sys.argv[1])
manifests = list((installation / "releases").glob("*/release-manifest.json"))
assert len(manifests) == 1, manifests
release = manifests[0].parent
manifest = json.loads(manifests[0].read_text(encoding="utf-8"))
root = pathlib.Path(sys.argv[2])
root.mkdir(parents=True)
image = root / "image"
image.mkdir(parents=True)
(image / "marker").write_text("artifact-security fixture\n", encoding="utf-8")
(image / "Dockerfile").write_text("FROM scratch\nCOPY marker /marker\nUSER 65532:65532\n", encoding="utf-8")
config = {
    "version": 4,
    "project": {"kind": "content"},
    "modules": [{"name": "fixture", "paths": ["image/**"]}],
    "tests": {"ownership": [], "suites": [{
        "name": "image-build", "kind": "integration", "scope": "module", "modules": ["fixture"],
        "argv": ["docker", "build", "--check", "image"],
    }]},
    "supplyChain": {"artifactSecurity": {"targets": [{
        "name": "minimal", "module": "fixture", "mode": "dockerfile",
        "dockerfile": "image/Dockerfile", "context": "image",
    }]}},
}
(root / ".code-polishy.json").write_text(json.dumps(config) + "\n", encoding="utf-8")
hosts = ["darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "windows-x64"]
lock = {
    "lockVersion": 2,
    "codePolishyVersion": manifest["codePolishyVersion"],
    "releaseDigest": manifest["releaseDigest"],
    "features": manifest["features"],
    "publication": {
        "indexUrl": "https://example.invalid/release-index.json", "indexSha256": "a" * 64,
        "archives": [{"host": host, "url": f"https://example.invalid/{host}.zip", "sha256": "b" * 64, "size": 1} for host in hosts],
    },
}
(root / ".code-polishy.lock.json").write_text(json.dumps(lock) + "\n", encoding="utf-8")
subprocess.run(["git", "-C", str(root), "init", "--quiet"], check=True)
subprocess.run([str(installation / "bin/code-polishy"), "--repo-root", str(root), "artifact-security"], check=True, timeout=1200)
version = (release / "tools/trivy-version.txt").read_text(encoding="utf-8").strip()
reports = root / ".code-polishy-reports/artifact-security"
for target in ("code-polishy-scanner", "minimal"):
    paths = list((reports / target).glob("*/report.json"))
    assert len(paths) == 1, paths
    report = json.loads(paths[0].read_text(encoding="utf-8"))
    assert report["target"] == target and report["scannerVersion"] == version, report
    assert not report["findings"], report
    if target == "minimal":
        assert not report["acceptedFindings"], report
    sbom = json.loads((paths[0].parent / "sbom.cdx.json").read_text(encoding="utf-8"))
    assert sbom["bomFormat"] == "CycloneDX" and sbom["specVersion"], sbom
print("Installed scanner and minimal image pass artifact security with fresh advisory data.")
PY
