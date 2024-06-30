from node:21 as tailwind

WORKDIR /build
COPY . /build
RUN npm install && npx tailwind -i ./css/input.css -o ./css/output.css

from golang:1.22 as builder

WORKDIR /build
COPY --from=tailwind /build /build

RUN go install github.com/a-h/templ/cmd/templ@v0.2.707
RUN templ generate
RUN CGO_ENABLED=0 go build -o ./bin/trigger2mqtt

FROM scratch

COPY --from=builder /build/bin /app/bin

ENTRYPOINT ["/app/bin/trigger2mqtt"]

