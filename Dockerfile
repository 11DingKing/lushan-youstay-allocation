# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.25.0-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/youstay ./cmd/server \
    && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/youstay /app/youstay
COPY --from=build --chown=nonroot:nonroot /out/data /app/data
VOLUME ["/app/data"]
ENV LISTEN_ADDR=:8080 DATABASE_PATH=/app/data/youstay.db
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/youstay"]
