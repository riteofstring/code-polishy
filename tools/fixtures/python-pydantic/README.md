# Installed Pydantic regression dependencies

The installed-release `python-pydantic` fixture copies these inputs to a disposable
repository, reviews the dependency candidate, and installs only matching wheels
whose bytes match the frozen lock. It requires network access to the recorded
package artifacts and runs no package build or installation scripts.

The fixture tests each reported model shape through the installed checker and a
real Pydantic serialization test. Ordinary unused members remain blocking.
