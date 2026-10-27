---
title: Kodi instances
description: Prepare Kodi, add Kodi instances to Encore, and set up Wake-on-LAN and the default instance.
---

Encore controls Kodi over its JSON-RPC API. It prefers Kodi's WebSocket, which also delivers live notifications (playback changes, library updates), and falls back to HTTP when the WebSocket isn't available. You can add as many Kodi instances as you like.

## Prepare Kodi

On each Kodi, open **Settings** → **Services** → **Control** and turn on:

- **Allow remote control via HTTP.** Note the **Port** (8080 by default) and set a **Username** and **Password**.
- **Allow remote control from applications on other systems.** This opens Kodi's JSON-RPC port (9090), which Encore uses for the WebSocket. Without it, Encore works over HTTP only and won't see new titles or playback changes as they happen.

For discovery, also turn on **Settings** → **Services** → **General** → **Announce services to other systems** (Zeroconf).

Encore must be able to reach both ports, so check any firewall on the Kodi host.

## Add an instance

On the **Kodi** page, Encore lists Kodi instances it finds on your network with mDNS (Kodi announces itself as `_xbmc-jsonrpc._tcp` and `_xbmc-jsonrpc-h._tcp`). Pick one, or add one by address:

- **Name**, e.g. "Lounge". It must be unique.
- **URL**: the HTTP JSON-RPC endpoint, e.g. `http://192.168.1.20:8080/jsonrpc`. Credentials written into the URL are moved to the username and password fields.
- **WebSocket URL**: e.g. `ws://192.168.1.20:9090/jsonrpc`. Leave it empty to use HTTP only.
- **Username** and **password** of Kodi's web server. The password is encrypted at rest.
- **MAC address** and **broadcast address** for Wake-on-LAN (optional, see below).
- **Title language**: the rules for matching spoken titles against this library (articles, number words and so on). Instances sharing a library use their library source's.
- **Time zone** of the Kodi host, e.g. `Australia/Melbourne`, which Kodi writes library dates in. Leave it empty and Encore learns the offset from Kodi's clock.
- **"Shut down"**: follow the setting under **Settings** → **Features**, or always quit Kodi or always power off for this instance (see below).

**Test** checks that Kodi answers, that the WebSocket connects, that the credentials work and that the library can be read. Each instance card shows its state: *connected* (WebSocket up), *HTTP only*, *asleep* or *offline*, along with what's playing, library counts and the newest title.

When Encore first connects it loads the whole library, then keeps it current from Kodi's notifications, a check for recently added items, and a periodic safety net. **Sync** reloads the library by hand.

mDNS doesn't cross Docker's default bridge network; with a bridged container, add instances by address or use host networking.

## Declare instances in config

Instances can also be declared as code. They are created or updated at startup, matched by name, and shown in the web interface as managed by config (read-only there):

```yaml title="encore.yml"
kodi:
  - name: Lounge
    url: http://192.168.1.20:8080/jsonrpc
    websocket_url: ws://192.168.1.20:9090/jsonrpc
    username: kodi
    password: change-me
    default: true
  - name: Bedroom
    url: http://192.168.1.21:8080/jsonrpc
    websocket_url: ws://192.168.1.21:9090/jsonrpc
    username: kodi
    password: change-me
    mac: "DC:A6:32:00:00:01"
```

In config, `language` is the title-matching rules for that library (`en` by default; `de`, `fr`, `es` and `it` have their own), `shutdown_quits` the "shut down" choice and `timezone` the time zone; `library_group` exists only in config (see [shared libraries](/encore/guides/shared-libraries/)). Kodi writes library dates (such as when a title was added) in its host's local time: set the time zone to an IANA name such as `Australia/Melbourne`, or leave it empty and Encore learns the host's UTC offset from Kodi's clock, rechecking it every six hours. See [`kodi`](/encore/reference/configuration/#kodi) in the configuration reference.

## The default instance

An Echo that isn't linked to a Kodi instance is answered by the **default** instance. Choose it on the Kodi page, or set `default: true` on one configured instance. Without a default, unlinked Echos hear "This Echo isn't linked to a Kodi yet." With a single Kodi, making it the default is all the linking you need. See [Echo devices and rooms](/encore/guides/echo-devices/) for linking Echos individually.

## Wake-on-LAN

If a Kodi host is asleep when a request arrives, Encore sends Wake-on-LAN packets, waits up to five seconds for Kodi to answer, and then carries out the request. **Wake** on the instance card does the same by hand.

- Encore learns the MAC address from Kodi once connected, so you only need to enter it if Kodi has never been reachable.
- Packets go to `255.255.255.255:9` unless you set a **broadcast** address, such as your LAN's directed broadcast `192.168.1.255`.
- Wake-on-LAN must be enabled on the host (in its firmware and network driver), and the packets must reach its network segment. From a container on Docker's bridge network, the limited broadcast doesn't reach the LAN; use host networking or a directed broadcast address.
- A cold boot may take longer than Alexa will wait. If the first request times out, try again once Kodi is up.

## "Shut down" and desktops

"Alexa, ask encore to shut down" powers off the Kodi host. For Kodi running on a desktop computer, where you'd rather just close Kodi, turn on **"Shutdown" quits Kodi** in **Settings** → **Features** ([`alexa.shutdown_quits`](/encore/reference/configuration/#alexashutdown_quits)). Each instance can override it with **"Shut down"** in its dialog, or `shutdown_quits` for an instance declared in config.
