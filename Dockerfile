FROM golang:1.22.6 AS build-stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY ./cmd ./cmd
COPY ./internal ./internal
RUN CGO_ENABLED=0 go build -o /monitoring ./cmd/monitoring

FROM scratch AS build-release-stage

WORKDIR /

COPY --from=build-stage /monitoring /monitoring
COPY monitoring-targets.yml ./

COPY --from=golang /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

EXPOSE 10050

ENTRYPOINT ["/monitoring"]
