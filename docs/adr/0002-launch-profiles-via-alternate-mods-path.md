# Launch profiles through SMAPI's alternate mods path

Each Profile owns a physical mods directory, and Astradew launches SMAPI pointed at it rather than renaming the game's global `Mods` folder. Renaming makes profiles a sequence of destructive global switches; a dedicated path lets every profile exist independently. SMAPI accepts the path as `--mods-path` on Windows, while the official documentation states command-line arguments do not work on Linux and macOS, where `SMAPI_MODS_PATH` is the supported equivalent — so the launch contract carries both channels.

## Consequences

An alternate path replaces the game's `Mods` folder entirely: SMAPI scans only the configured path, and the bundled Console Commands and Save Backup folders that the SMAPI installer places in `Mods` are not discovered automatically in a Profile.
