ARG SCANNER_IMAGE
FROM ${SCANNER_IMAGE} AS source
FROM scratch
COPY --from=source /usr/local/bin/trivy /usr/local/bin/trivy
COPY --from=source /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENV GODEBUG=http2client=0
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/trivy"]
