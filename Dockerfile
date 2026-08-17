FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/voxellink-monitor ./cmd/monitor

FROM cloudflare/cloudflared:2026.7.3 AS cloudflared

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/voxellink-monitor /voxellink-monitor
COPY --from=cloudflared /usr/local/bin/cloudflared /usr/local/bin/cloudflared
USER nonroot:nonroot
ENTRYPOINT ["/voxellink-monitor"]
