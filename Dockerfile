FROM alpine
COPY features features
COPY web /
ENTRYPOINT ["/web"]
EXPOSE 8090
