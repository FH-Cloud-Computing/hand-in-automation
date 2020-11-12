FROM alpine
COPY features features
COPY web /
ENTRYPOINT ["/web"]
EXPOSE 8090
USER 1000:1000
