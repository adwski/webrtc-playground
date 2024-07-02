FROM golang:1.22.3-bookworm as builder

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GO111MODULE=on \
    GOARCH=amd64 \
    GOPATH=/go

ADD . /build

WORKDIR /build

RUN go mod download
RUN <<EOF
    go build -o app -ldflags '-d -w -s' -tags netgo -installsuffix netgo ./backend/cmd/app.go
    chmod +x /build/app
EOF


FROM builder as dev

ENTRYPOINT ["/build/app"]
