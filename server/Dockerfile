# routing-core als statisches Binary in einem leeren Image. Gebaut nur im CI (D-03), nie auf dem OptiPlex.
# Basis-Image mit festem Tag + Digest; beim Update beides anpassen.
FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /routing-core ./cmd/routing-core

FROM scratch
LABEL org.opencontainers.image.source="https://github.com/Lord3n20/SchnellWeg" \
      org.opencontainers.image.description="SchnellWeg routing-core: RAPTOR-ÖPNV-Routing für die Region Aachen"
COPY --from=build /routing-core /routing-core
# CA-Zertifikate für HTTPS-Abrufe (Echtzeit-Feed, D-15); das leere Image hat sonst keine
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
EXPOSE 8090
ENTRYPOINT ["/routing-core", "-options", "/data/options.json", "-dir", "/config"]
