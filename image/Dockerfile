FROM golang:1.19.1

ARG USER_ID
ARG GROUP_ID
RUN addgroup --gid ${GROUP_ID} app \
 && adduser --disabled-password --uid ${USER_ID} --ingroup app --gecos '' app

WORKDIR /usr/src/app

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN go build -v -o /usr/local/bin/app ./app

USER app

CMD ["app"]
