FROM golang:1.26.5-bookworm@sha256:53eeac89074db483fdf0ab3be1df32bf6e47562263d2d0d6baa7f26acb4957dd AS build
WORKDIR /source
COPY go.mod go.sum ./
RUN go mod download
COPY gen gen
COPY internal internal
COPY cmd cmd
RUN CGO_ENABLED=0 go build -trimpath -o /out/superverse-server ./cmd/superverse-server

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/superverse-server /superverse-server
EXPOSE 8080 8082 50051
ENTRYPOINT ["/superverse-server"]
