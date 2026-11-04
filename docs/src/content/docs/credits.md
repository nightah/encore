---
title: Credits
description: The people and projects Encore builds on.
---

## Kanzi

Encore is a successor to [Kanzi](https://github.com/m0ngr31/kanzi) by m0ngr31, the Alexa skill that showed how good controlling Kodi by voice could be. Its voice commands, and settings such as deep search and the playlist limit, carry on here. Kanzi has been archived.

## Go gopher

Encore's logo is an original rendition of the Go gopher. The Go gopher was designed by Renée French and is licensed under the [Creative Commons Attribution 4.0 License (CC BY 4.0)](https://creativecommons.org/licenses/by/4.0/). See [brand](/encore/brand/) for the full attribution.

## Kodi

Encore works with [Kodi](https://kodi.tv/) through its JSON-RPC API. It is not affiliated with or endorsed by the XBMC Foundation, and uses no Kodi logo or branding.

## Ported code

Some of Encore's matching code is ported from other projects, whose licences ask that their notices travel with Encore:

- **Double Metaphone** (`internal/fuzzy/phonetic/en`) is derived from [PhonoGrams/double_metaphone](https://github.com/PhonoGrams/double_metaphone) v0.1.0, under the Apache License 2.0, as Encore is.
- **Kölner Phonetik** (`internal/fuzzy/phonetic/de`) is derived from [f1monkey/phonetic](https://github.com/f1monkey/phonetic) v1.4.2, under the MIT License; its notice is kept with the code.
- **TinySegmenter** (`internal/fuzzy/tinyseg`), which splits Japanese titles and requests into words, is a Go port of [TinySegmenter 0.2](http://chasen.org/~taku/software/TinySegmenter/) by Taku Kudo, with its weights unchanged, under this licence:

```text
Copyright (c) 2008, Taku Kudo

All rights reserved.

Redistribution and use in source and binary forms, with or without modification, are permitted provided that the following conditions are met:

    * Redistributions of source code must retain the above copyright notice, this list of conditions and the following disclaimer.
    * Redistributions in binary form must reproduce the above copyright notice, this list of conditions and the following disclaimer in the documentation and/or other materials provided with the distribution.
    * Neither the name of the <ORGANIZATION> nor the names of its contributors may be used to endorse or promote products derived from this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

The French, Spanish, Brazilian Portuguese and Arabic encoders are Encore's own, written from each language's published spelling-to-sound descriptions, cited in each package's documentation.

## Licence

Encore is licensed under the [Apache License 2.0](https://github.com/nightah/encore/blob/main/LICENSE). This documentation is built with [Astro Starlight](https://starlight.astro.build/).
