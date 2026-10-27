# Pinned Kodi JSON-RPC schemas

Downloaded read-only from `xbmc/xbmc`, path `xbmc/interfaces/json-rpc/schema/`, via `https://raw.githubusercontent.com/xbmc/xbmc/<commit>/xbmc/interfaces/json-rpc/schema/<file>`. Files are stored byte-for-byte; do not edit them. To bump a version, download the four files for the new tag into a new directory, update this table and the `go:generate` line in `internal/backend/kodi/kodirpc/generate.go`.

| Dir | Tag | Commit | API version |
|-----|-----|--------|-------------|
| `omega/` | `21.3-Omega` | `a3a448d26b8d560a65655dab2cd122994dc4e146` | 13.5.0 |
| `piers/` | `22.0rc1-Piers` | `28ea2eac1eb7af8fdcbd2672d933ce87f594be79` | 13.1000.0 |

## SHA-256

```
9df7592690d30140e62b3b94cc34af16eb056412436939ff87792c951a21357f  omega/methods.json
efe53306202de412352c4c9199e5ff6bb5bb60f05c7f98d7384f23ed405cbbb4  omega/notifications.json
09463e68e3863e701eeee2e0e0b8d14f0a51c5cc8ea1275670fb044e1f1e7420  omega/types.json
8431e1ceb6ffac4a81eb580b0f5b918fa71b8d3f9203c4caa1330250ef4d6ff1  omega/version.txt
b14e3cc56593ef0c7d2ce1cea7908c8ba433ab71f967f2ead36e329ca4716eaf  piers/methods.json
79bece8882c57b83c7e5786b21c5c4431902dad9c80e2283c114042863c8ace3  piers/notifications.json
a4c101608b29efc8a742ab6a0e7c3010a059a0421d5f7537cca80b93d0f143ca  piers/types.json
cea5d75e716995c4f7682ae52aaa8d9ff1368b108712f7ba35976d3dc7a5df5b  piers/version.txt
```
