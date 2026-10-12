# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.24 AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/carbonara ./cmd/carbonara

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/carbonara /carbonara
EXPOSE 8080
ENTRYPOINT ["/carbonara"]
CMD ["serve"]
