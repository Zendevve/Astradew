# Store library content by hash

Packages and Archives live under content-addressed paths derived from their SHA-256, with every human-facing name held in SQLite. Identical bytes collapse to one directory, garbage collection reduces to a reachability check, a mod renaming itself never cascades into filesystem renames, and no reserved or non-ASCII name reaches a path. The trade-off is a tree that is not browsable by hand, accepted because the database is the index and the interface is the human view.
