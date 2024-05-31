# wadoh-be
Device backend for [wadoh](https://github.com/9d4/wadoh). It holds device data and provides
gRPC interface to communicate. Currently the device data stored in sqlite.

## Running
It should work out of the box without any configuration, but can be set if needed.
Following are the supported environment variables.
```sh
DEVELOPMENT=true
DB_PATH=mystore.db  #default to store.db
GRPC_PORT=50051
```

Run with
```sh
$ go run .
```
