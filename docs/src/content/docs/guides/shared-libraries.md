---
title: Shared libraries
description: How Encore detects Kodi instances that share a MySQL/MariaDB library, and how to choose yourself.
---

Many households point several Kodi instances at one [MySQL or MariaDB library database](https://kodi.wiki/view/MySQL), so watched status and resume points follow you from room to room. Encore notices this and gives those instances **one** library index between them: the library is loaded once, titles are matched once, and the skill model counts each title once however many instances show it.

## Automatic detection

Detection never writes to Kodi or its library. It works in two stages.

1. **Fingerprint.** Once an instance is reachable, Encore samples up to 200 movies and 200 episodes from its library: their library IDs, file paths and dates added. Two instances whose samples agree on at least 95% of at least 20 items are grouped as *likely shared*. (On Kodi versions that report their database name, instances naming different databases are never grouped.)
2. **Passive confirmation.** Encore then watches playback. When one member saves a play count or resume point, every other member reading the same database must show the same change a moment later. Members that do are *confirmed shared*; a member that doesn't is split off and won't be grouped with that library again.

A group's state is shown on the **Kodi** page: *solo*, *likely shared*, *confirmed shared* or *manual*, with the evidence ("200 of 200 sampled items match").

## Library source and failover

Each group syncs its library from one member, the **source**: by default the instance added first, or the one you choose on the Kodi page. While the source is asleep or unreachable, Encore syncs from another reachable member, so a sleeping Kodi isn't woken just to read the library, and switches back when the source returns. Library notifications from every member are used, since with a shared database only the Kodi that made a change reports it.

Voice requests still play on the Echo's own Kodi instance: sharing a library only changes where titles are looked up.

## Choosing yourself

On the Kodi page, **Share a library** groups instances that detection missed (the first one you tick becomes the source), **Add an instance** on a shared library's panel adds another, and **Not shared? Split** takes one out. Detection never undoes a choice made by hand.

In config, `library_group` decides instead:

```yaml title="encore.yml"
kodi:
  - name: Lounge
    url: http://192.168.1.20:8080/jsonrpc
    library_group: house                   # same name: one shared library
  - name: Bedroom
    url: http://192.168.1.21:8080/jsonrpc
    library_group: house
  - name: Garage
    url: http://192.168.1.22:8080/jsonrpc
    library_group: none                    # always its own library, even if it looks shared
```

- Instances with the same `library_group` name share one library.
- `none` keeps an instance on its own.
- Leaving it empty lets Encore detect sharing.

Groups set in config are read-only in the web interface.

## Library snapshots

After syncs Encore saves each library's catalog to the configured [storage](/encore/guides/storage/) (at most every five minutes, and on shutdown). After a restart the snapshot is loaded first, so voice requests work before Kodi has been asked again; the live sync that follows corrects anything that changed.
