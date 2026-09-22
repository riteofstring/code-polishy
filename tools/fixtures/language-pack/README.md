# SQLite syntax proof

This small protocol v4 pack proves that the same core engine can run an analyzer
for a language it does not implement. Its extensionless Node adapter asks the
policy-owned toolchain's SQLite parser to prepare comment-free SELECT statements
in an in-memory database, without executing them. The two fixtures distinguish
valid SQL from a real syntax error. Other SQL constructs remain unsupported.

The `examples` directory contains protocol v4 request and response documents.
The pack unit suite checks those examples against the published schemas and the
same production encoder, decoder, and semantic validator used during execution.

The pack supplies lint only. It explicitly records formatting as unsupported and
cannot satisfy type checking, complexity, architecture, or unused-code requirements,
so it is not a full SQL pack.
