FROM golang:alpine

WORKDIR /usr/src/app

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN go build -v -o /usr/local/bin/app ./app

FROM alpine

ARG USER_ID
ARG GROUP_ID
RUN addgroup --gid ${GROUP_ID} app \
 && adduser --disabled-password --uid ${USER_ID} --ingroup app --gecos '' app

COPY --from=0 /usr/local/bin/app /usr/local/bin/app
COPY --from=0 /usr/src/app/dist/datasync /usr/local/bin/dist/datasync

USER app

CMD ["app", "100"]
