protoc --proto_path ../../../ -I=./proto --go_out=plugins=grpc:./proto proto/proxy.proto
mv proto/github.com/brotherlogic/proxy/proto/* ./proto
