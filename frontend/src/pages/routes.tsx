export interface RouteDef {
  /** Hash path without the leading "#", e.g. "/library". */
  path: string;
  /** Navigation label and accessible link name. */
  label: string;
  /** Page heading. */
  heading: string;
  /**
   * Honest empty-state copy: states what is missing and what will fill it.
   * No sample mods, fake libraries, or placeholder numbers.
   */
  empty: string;
}

export const ROUTES: RouteDef[] = [
  {
    path: "/",
    label: "Dashboard",
    heading: "Dashboard",
    empty:
      "No game is configured yet. Configure a game in Settings and its status will appear here.",
  },
  {
    path: "/library",
    label: "Library",
    heading: "Library",
    empty: "No mods installed. Mods you add will be listed here.",
  },
  {
    path: "/profiles",
    label: "Profiles",
    heading: "Profiles",
    empty:
      "No profiles yet. Create a profile to keep separate mod setups.",
  },
  {
    path: "/updates",
    label: "Updates",
    heading: "Updates",
    empty:
      "Nothing to check for updates. Updates appear here once a game with mods is configured.",
  },
  {
    path: "/health",
    label: "Health",
    heading: "Health",
    empty:
      "No health information. Mod health checks run once a game with mods is configured.",
  },
  {
    path: "/downloads",
    label: "Downloads",
    heading: "Downloads",
    empty: "No downloads. Queued and finished downloads will appear here.",
  },
  {
    path: "/logs",
    label: "Logs",
    heading: "Logs",
    empty: "No log entries yet. Application activity will be recorded here.",
  },
  {
    path: "/settings",
    label: "Settings",
    heading: "Settings",
    empty:
      "No settings available yet. Configurable options will appear here once a game is configured.",
  },
];
