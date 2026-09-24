module example.com/payments

go 1.22

require google.golang.org/grpc v1.58.0

replace google.golang.org/grpc => ./third_party/grpcstub
