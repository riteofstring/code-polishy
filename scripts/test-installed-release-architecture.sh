#!/usr/bin/env bash

declare output

installed_fixture_python() {
  local release="$1" candidate
  for candidate in "${release}"/.tools/python/*/python "${release}"/.tools/python/*/python.exe; do
    if [[ -x "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done
  fail "installed architecture fixture: the release carries no Python interpreter"
}

fixture_accept_architecture() {
  local target="$1" base="$2" python="$3"
  expect_pass "${target}" "fixture architecture packet" architecture-review prepare --base "${base}" --format json
  "${python}" -I -B - "${target}" "${output}" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
prepared = json.loads(pathlib.Path(sys.argv[2]).read_text())["architecturePreparation"]
packet = json.loads((root / prepared["packetPath"]).read_text())
index, source = next((index, source) for index, source in enumerate(packet["sources"]) if source["content"].strip())
result = {
    "protocol": packet["protocol"], "reviewId": packet["reviewId"],
    "base": packet["base"], "candidate": packet["candidate"],
    "topology": packet["topology"]["identity"], "decision": "accept",
    "rationale": "This integration-test result exercises acceptance of the fixture's declared source ownership.",
    "evidence": [{"pointer": f"/sources/{index}/content", "quote": source["content"],
                  "rationale": "The exact candidate source binds this integration-test result to the prepared packet."}],
    "findings": [],
}
(root / prepared["resultPath"]).write_text(json.dumps(result) + "\n")
PY
  expect_pass "${target}" "fixture architecture receipt" architecture-review finalize --base "${base}"
}
