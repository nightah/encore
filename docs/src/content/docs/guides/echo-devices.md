---
title: Echo devices and rooms
description: Link each Echo to a Kodi instance, group them into rooms, and target a room by voice.
---

With more than one Kodi, Encore needs to know which one each Echo controls. It learns about Echos as they talk to it, and you link them on the **Echo devices** page.

## How an Echo is matched to a Kodi

For every request Encore picks a Kodi instance in this order:

1. **A room named in the request.** "Play Dune in the bedroom" plays on the bedroom's Kodi, whichever Echo heard it.
2. **The Echo's linked instance.**
3. **The default instance** for unlinked Echos (see [Kodi instances](/encore/guides/kodi/#the-default-instance)).

If none applies, Alexa answers "This Echo isn't linked to a Kodi yet."

## Link an Echo

An Echo appears on the **Echo devices** page the first time it sends Encore a request, and the overview flags new, unlinked ones. Say anything to the skill from that Echo ("Alexa, open encore"), then on its card:

- give it a **name** you'll recognise in the request log, such as "Lounge Echo Show",
- choose the **Kodi instance** it controls,
- optionally put it in a **room**.

The page also shows when each Echo was first and last seen and how many requests it has made. Forgetting an Echo removes it until it talks to Encore again.

## Rooms

A room has a name, an optional Kodi instance that plays there, and spoken **aliases**: other names people use for it ("living room" and "family room" for the lounge). Names and aliases must be unique across rooms.

Requests that start playback accept a room at the end: "Alexa, ask encore to play the next episode of Severance in the bedroom", "... put on some jazz in the kitchen". Encore ignores a leading "the", matches the name against rooms and their aliases, and plays on that room's Kodi. A room without a Kodi instance falls back to the Echo's own instance; a name that matches no room gets "I don't know a room called …". The [voice commands](/encore/voice-commands/) pages show which commands take a `{Room}`.

Rooms work as soon as you add them. They also become slot values the next time you [deploy the skill](/encore/guides/deploy-skill/#redeploying), which helps Alexa hear unusual room names.

## Declare rooms and devices in config

For [stateless deployments](/encore/guides/stateless/) or if you prefer code, declare rooms and Echo links in config. Rooms are matched by name (their aliases are made to match exactly) and devices by their Alexa device ID; rooms and devices not in config are left alone.

```yaml title="encore.yml"
kodi:
  - name: Lounge
    url: http://192.168.1.20:8080/jsonrpc
  - name: Bedroom
    url: http://192.168.1.21:8080/jsonrpc
rooms:
  - name: Lounge
    kodi: Lounge
    aliases: [living room, family room]
  - name: Bedroom
    kodi: Bedroom
devices:
  - id: amzn1.ask.device.EXAMPLE-DEVICE-ID
    name: Lounge Echo Show
    kodi: Lounge
    room: Lounge
```

The Alexa device ID (`amzn1.ask.device.…`) of a new Echo is shown in the [request log](/encore/guides/request-log/) and on its card. A room's or device's `kodi` must name an instance in the `kodi` list.
