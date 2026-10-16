# Mira agent. Two stages: build a static binary with the Go image, then run it
# on distroless/static, which holds only CA certificates (for HTTPS to model
# APIs, Telegram, calendars), time zone data and a non-root user.
#
#   docker build -t mira .
#   docker run -v ./mira.yaml:/etc/mira/mira.yaml:ro -v mira-data:/var/lib/mira \
#     -p 8787:8787 -e TZ=Europe/Berlin mira
#
# The config sets store.path: /var/lib/mira/mira.db (as in mira.example.yaml).
# MCP servers over stdio need their runtime (e.g. node) and do not run in this
# image; use MCP servers over HTTP.

FROM golang:1.27.2 AS build
# VERSION overrides the version in the code (the release sets it from the tag).
ARG VERSION=
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w ${VERSION:+-X main.version=${VERSION}}" -o /out/mira ./cmd/mira
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/mira /app/mira
COPY skills /app/skills
COPY --from=build --chown=nonroot:nonroot /out/data /var/lib/mira
ENV MIRA_CONFIG=/etc/mira/mira.yaml
VOLUME /var/lib/mira
EXPOSE 8787
ENTRYPOINT ["/app/mira"]
CMD ["run"]
