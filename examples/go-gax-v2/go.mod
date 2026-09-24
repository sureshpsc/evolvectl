module example.com/speech

go 1.22

require github.com/googleapis/gax-go v0.0.0-20161107002406-da06d194a00e

replace github.com/googleapis/gax-go => ./third_party/gaxv0stub

replace github.com/googleapis/gax-go/v2 => ./third_party/gaxv2stub
