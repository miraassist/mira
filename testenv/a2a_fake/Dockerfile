# The fake A2A agent (docs/spec/fake-devices.md), built from the repository's
# root: docker build -f testenv/a2a_fake/Dockerfile . (testenv/compose.yaml
# does it). Dockerfile.dockerignore beside it keeps the context small.
FROM golang:1.27.3 AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY testenv/a2a_fake testenv/a2a_fake
RUN CGO_ENABLED=0 go build -trimpath -o /out/a2a-fake ./testenv/a2a_fake/cmd/a2a-fake

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/a2a-fake /a2a-fake
EXPOSE 8080
ENTRYPOINT ["/a2a-fake"]
