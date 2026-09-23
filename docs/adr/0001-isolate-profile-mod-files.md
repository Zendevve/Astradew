# Give each profile independent writable mod files

Each profile owns an independent deployed copy of its mod files. Shared writable mod directories, including symlink-based sharing, are rejected because mods can write configuration, caches, and other state beside their binaries; the extra disk usage and copy time are accepted to prevent one profile from changing another.
