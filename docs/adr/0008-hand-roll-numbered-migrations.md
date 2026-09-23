# Hand-roll numbered database migrations

Schema changes ship as numbered `.sql` files embedded in the binary and applied through a `schema_migrations` table, rather than through a migration library. The pre-migration backup and the "test against a previous production database fixture" requirement have to be written regardless, so a library would only add a dependency to wrap, and the PRD asks for minimal dependencies. The migrator stays small enough to read in one sitting, and it never mutates the schema outside a numbered migration.
