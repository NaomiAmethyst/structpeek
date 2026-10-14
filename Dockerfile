# syntax=docker/dockerfile:1
# SPDX-License-Identifier: GPL-3.0-or-later
# Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>
#
#   docker run --rm -i ghcr.io/naomiamethyst/structpeek < data.json
#   docker run --rm -v "$PWD:/data:ro" ghcr.io/naomiamethyst/structpeek data.yaml

# Cross-compiles on the build machine for each target platform.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH TARGETVARIANT
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/structpeek ./cmd/structpeek

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/structpeek /usr/local/bin/structpeek
WORKDIR /data
ENTRYPOINT ["structpeek"]
