# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/cachepotd ./cmd/cachepotd

FROM gcr.io/distroless/static-debian12:nonroot AS final
COPY --from=build /out/cachepotd /cachepotd
EXPOSE 6380 6381
USER nonroot:nonroot
ENTRYPOINT ["/cachepotd"]
