FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/voxellink-monitor ./cmd/monitor

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/voxellink-monitor /voxellink-monitor
USER nonroot:nonroot
ENTRYPOINT ["/voxellink-monitor"]

