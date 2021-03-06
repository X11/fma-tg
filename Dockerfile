FROM golang
RUN mkdir /go/src/github.com/X11/fma-tg -p
COPY . /go/src/github.com/X11/fma-tg
WORKDIR /go/src/github.com/X11/fma-tg
ENV BOT_ENV "production"
VOLUME /data
RUN go get
RUN go install
ENTRYPOINT /go/bin/fma-tg
