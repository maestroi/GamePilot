# syntax=docker/dockerfile:1
#
# GamePilot serve image: public spectator :8080 and private operator :8081.
# The Tetris ROM is never part of this image. Bind-mount it at runtime.

FROM golang:1.26 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/gamepilot ./cmd/gamepilot

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && update-ca-certificates
COPY --from=build /out/gamepilot /usr/local/bin/gamepilot
USER nobody
ENTRYPOINT ["gamepilot"]
CMD ["-planner", "serve", "-rom", "/roms/tetris.gb", "-public-addr", ":8080", "-private-addr", ":8081"]
