FROM golang:1.22.6 AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY ./cmd ./cmd
COPY ./internal ./internal
RUN CGO_ENABLED=0 go build -o /zcm ./cmd/zcm

FROM scratch AS release

WORKDIR /

COPY --from=build /zcm /zcm
COPY --from=golang /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

EXPOSE 10050

ENTRYPOINT ["/zcm"]

FROM golang:1.22.6 AS develop

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY monitoring-targets.yml ./
COPY ./cmd ./cmd
COPY ./internal ./internal

EXPOSE 10050

ENV CGO_ENABLED=0
ENTRYPOINT ["go", "run", "./cmd/zcm"]
