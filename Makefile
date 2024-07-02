tls:
	openssl req -x509 -nodes -days 365 -newkey rsa:4096 \
	-keyout ./docker/tls/key.pem -out ./docker/tls/cert.pem \
	-subj "/C=XX/O=test/OU=test/CN=test" \
		-addext "subjectAltName = DNS:localhost, IP:127.0.0.1, IP:::1, IP:192.168.1.122"
