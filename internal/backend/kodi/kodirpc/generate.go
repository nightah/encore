package kodirpc

//go:generate sh -c "cd ../../../../tools && go run ./kodigen -schemas kodigen/schema -matrix ../.github/workflows/kodi-contract.yml -out ../internal/backend/kodi/kodirpc -releases ../internal/backend/kodi/contract_releases_gen_test.go"
