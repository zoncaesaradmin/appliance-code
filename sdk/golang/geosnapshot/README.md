# Geo snapshot SDK

`geosnapshot` is the shared producer/consumer contract for Geo-Zone snapshots.

Consumers provide an S3-compatible `ObjectStore`, then verify and decode one
snapshot with:

```go
snapshot, err := geosnapshot.Read(ctx, objectStore, "geo/zones")
if err != nil {
	return err
}
result, err := geosnapshot.Load(ctx, geosnapshot.PostgresLoader{DB: db}, snapshot)
if err != nil {
	return err
}
_ = result
```

`Read` follows `current.json`, verifies the manifest and SHA-256 digest, limits
the decompressed payload, and parses stable zone IDs plus GeoJSON geometry.
`PostgresLoader` calls the canonical `api.import_zones(jsonb)` function, which
replaces the target set atomically.
