# Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License
FROM golang:alpine

WORKDIR /usr/src/app

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN go build -ldflags "-s -w" -v -o /usr/local/bin/app ./app

FROM alpine

RUN apk update && apk add ca-certificates && rm -rf /var/cache/apk/*
COPY ./USERTrust_RSA_Certification_Authority.pem /usr/local/share/ca-certificates/USERTrust_RSA_Certification_Authority.pem
RUN update-ca-certificates

ARG USER_ID
ARG GROUP_ID
RUN addgroup --gid ${GROUP_ID} app \
 && adduser --disabled-password --uid ${USER_ID} --ingroup app --gecos '' app

COPY --from=0 /usr/local/bin/app /usr/local/bin/app

USER app

CMD ["app", "100"]
