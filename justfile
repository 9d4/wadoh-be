default:
    just --list

proto-gen:
    protoc --go_out=. --go-grpc_out=. --proto_path=proto wadoh.proto

run:
    go run .
