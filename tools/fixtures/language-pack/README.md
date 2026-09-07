# SQLite syntax proof

This small protocol v2 pack proves that the same core engine can run an analyzer
for a language it does not implement. Its extensionless Node adapter asks the
policy-owned runtime's SQLite parser to prepare comment-free SELECT statements
in an in-memory database, without executing them. The two fixtures distinguish
valid SQL from a real syntax error. Other SQL constructs remain unsupported.

The pack supplies lint only. It cannot satisfy formatting, type checking,
complexity, architecture, or unused-code requirements and is not a full SQL pack.
