ARG ARCH=amd64
# using the SHA256 instead of tags
# https://github.com/opencontainers/image-spec/blob/main/descriptor.md#digests
# https://cloud.google.com/architecture/using-container-images
# https://github.com/google/go-containerregistry/blob/main/cmd/crane/README.md
# ➜  ~ crane digest golang:1.17.13-alpine3.16
# sha256:c80567372be0d486766593cc722d3401038e2f150a0f6c5c719caa63afb4026a
FROM golang@sha256:c80567372be0d486766593cc722d3401038e2f150a0f6c5c719caa63afb4026a AS build-container

ARG ARCH
ENV GO111MODULE=on

COPY proxy.go /app/proxy.go
WORKDIR /app
RUN  go build -o /go/bin/proxy -v proxy.go

FROM $ARCH/alpine:3.16

COPY --from=build-container /go/bin /bin

EXPOSE 8333 8334

ENTRYPOINT ["proxy"]
