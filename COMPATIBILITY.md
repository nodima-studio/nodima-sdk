# Compatibility policy

SDK releases are versioned independently from the runner ABI. Each package
manifest pins its ABI explicitly. Additive SDK conveniences may ship without
changing that ABI; wire or manifest changes require a new ABI identifier and
parallel compatibility tests.

Legacy `dbminer.*` strings and the archive media type remain accepted for the
entire v1alpha1 lifecycle. Nodima Studio is the product and SDK name; those
legacy identifiers are stable serialized protocol values.
