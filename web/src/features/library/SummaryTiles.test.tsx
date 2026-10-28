import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Instance, Library, Settings } from "@api/generated/model";
import { SummaryTiles } from "@features/library/SummaryTiles";

const library = {
  counts: { movies: 2184, shows: 312, songs: 41907 },
  id: 3,
  last_sync_at: null,
  source_instance_id: 1,
} as unknown as Library;

const settings = (storage: Partial<Settings["storage"]>, lastBackup: string | null = null) =>
  ({
    storage: {
      backend: "local",
      backups: { last_backup_at: lastBackup },
      local: { path: "/var/lib/encore" },
      s3: { endpoint: "" },
      webdav: { url: "" },
      ...storage,
    },
  }) as unknown as Settings;

function tile(label: string) {
  const tiles = screen.getByRole("region", { name: "Library summary" });
  const heading = within(tiles).getByText(label);
  return heading.closest("div")?.parentElement ?? heading;
}

describe("SummaryTiles", () => {
  it("counts the library and names its source's episodes", () => {
    const instances = [{ id: 1, library: { episodes: 1 } }] as unknown as Instance[];
    render(
      <SummaryTiles
        library={library}
        libraries={[library]}
        instances={instances}
        settings={undefined}
        settingsPending={false}
      />,
    );
    expect(screen.getByText("2,184")).toBeInTheDocument();
    expect(screen.getByText("1 episode")).toBeInTheDocument();
    expect(screen.getByText("41,907")).toBeInTheDocument();
  });

  it("leaves out episodes while instances load, and says when nothing has synced", () => {
    render(
      <SummaryTiles
        library={library}
        libraries={[library]}
        instances={undefined}
        settings={settings({ backend: "local" })}
        settingsPending={false}
      />,
    );
    expect(screen.queryByText(/episode/)).not.toBeInTheDocument();
    expect(screen.getByText("not synced yet")).toBeInTheDocument();
    expect(tile("Backups")).toHaveTextContent("/var/lib/encore · no backup yet");
  });

  it("shows the backups tile loading while the settings load", () => {
    render(
      <SummaryTiles library={library} libraries={[library]} instances={[]} settings={undefined} settingsPending />,
    );
    expect(within(tile("Backups")).getByRole("status")).toHaveTextContent("Loading…");
    expect(tile("Backups")).not.toHaveTextContent("no backup yet");
  });

  it("doesn't claim there's no backup when the settings didn't load", () => {
    render(
      <SummaryTiles
        library={library}
        libraries={[library]}
        instances={[]}
        settings={undefined}
        settingsPending={false}
      />,
    );
    expect(tile("Backups")).toHaveTextContent("-");
    expect(tile("Backups")).not.toHaveTextContent("no backup yet");
  });

  it("reports the most recent sync across all libraries", () => {
    const hourAgo = new Date(Date.now() - 3600_000).toISOString();
    const dayAgo = new Date(Date.now() - 86_400_000 * 3).toISOString();
    const libraries = [
      { ...library, last_sync_at: dayAgo },
      { ...library, id: 4, last_sync_at: hourAgo },
      { ...library, id: 5, last_sync_at: null },
    ];
    render(
      <SummaryTiles
        library={library}
        libraries={libraries}
        instances={[]}
        settings={undefined}
        settingsPending={false}
      />,
    );
    expect(screen.getByText("3")).toBeInTheDocument();
    expect(screen.getByText("synced 1 h ago")).toBeInTheDocument();
  });

  it.each([
    [{ backend: "s3", s3: { endpoint: "https://minio.example.net:9000" } }, "S3-compatible", "minio.example.net:9000"],
    [{ backend: "s3", s3: { endpoint: "" } }, "S3-compatible", "AWS S3"],
    [{ backend: "webdav", webdav: { url: "https://dav.example.net/remote.php/dav" } }, "WebDAV", "dav.example.net"],
    [{ backend: "local" }, "Local disk", "/var/lib/encore"],
  ] as const)("names where backups go (%j)", (storage, label, where) => {
    render(
      <SummaryTiles
        library={library}
        libraries={[library]}
        instances={[]}
        settings={settings(storage as Partial<Settings["storage"]>, new Date(Date.now() - 120_000).toISOString())}
        settingsPending={false}
      />,
    );
    expect(tile("Backups")).toHaveTextContent(label);
    expect(tile("Backups")).toHaveTextContent(`${where} · last backup 2 min ago`);
  });
});
