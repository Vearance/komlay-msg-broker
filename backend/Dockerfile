FROM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY . .

ARG SERVICE
RUN mkdir -p /out \
    && case "${SERVICE}" in \
      web-shop|billing|shipping) ;; \
      *) echo "unsupported SERVICE: ${SERVICE}" >&2; exit 1 ;; \
    esac \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
      -o /out/service "./cmd/${SERVICE}"

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/service /service
EXPOSE 8080
ENTRYPOINT ["/service"]
