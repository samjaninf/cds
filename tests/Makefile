define MINIO_CONFIG
export HOSTNAME=$$(hostname)
export AWS_DEFAULT_REGION=us-east-1
export S3_BUCKET=cds-it
export MINIO_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE
export MINIO_SECRET_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
export AWS_ENDPOINT_URL=http://$$(hostname):9000
endef

MINIO_CONTAINER_ID = $(shell docker ps -f name=minio1 -q)
GITEA_CONTAINER_ID = $(shell docker ps -f name=gitea -q)
GITEA_VERSION = 1.21

UNAME_S := $(shell uname -s| tr A-Z a-z)
VERSION := $(if ${CDS_VERSION},${CDS_VERSION},snapshot)

GITEA_USER := $(if ${GITEA_USER},${GITEA_USER},gituser)
GITEA_USER_FORK := $(if ${GITEA_USER_FORK},${GITEA_USER_FORK},gituserfork)
GITEA_PASSWORD := $(if ${GITEA_PASSWORD},${GITEA_PASSWORD},gitpassword)
GITEA_DOMAIN := $(if ${GITEA_DOMAIN},${GITEA_DOMAIN},localhost)
GITEA_SSH_PORT := $(if ${GITEA_SSH_PORT},${GITEA_SSH_PORT},2222)

COVER_FILES = $(shell find . -name "*.coverprofile")

clean:
	@rm -f $(MINIO_CONTAINER_ID) $(MINIO_RC)
	@docker kill minio1 || true && docker rm minio1 || true
	@docker kill gitea || true && docker rm gitea || true

minio_start: $(MINIO_RC)
	@if [ -z "$(MINIO_CONTAINER_ID)" ]; then \
		docker rm minio1 >/dev/null 2>&1 || true; \
		echo "starting minio container"; \
		docker run -d -p 9000:9000 --name minio1 \
				-e "MINIO_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE" \
				-e "MINIO_SECRET_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" minio/minio server /data; \
	fi; \
	$(info # Here are the minio configuration variables)
	$(info $(MINIO_CONFIG))

minio_reset_bucket:
	@docker run --rm --link minio1:minio --entrypoint sh minio/mc:RELEASE.2023-10-14T01-57-03Z -c "\
		microdnf update --nodocs; microdnf install nc --nodocs; \
		while ! nc -z minio 9000; do echo 'Wait minio to startup...' && sleep 0.1; done; \
		sleep 5 && \
		mc config host add myminio http://minio:9000 AKIAIOSFODNN7EXAMPLE wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY && \
		mc rm -r --force myminio/cds-it  || true && \
		mc mb myminio/cds-it || true \
		"

gitea_start:
	@if [ -z "$(GITEA_CONTAINER_ID)" ]; then \
  		docker run -d -p 3000:3000 -p ${GITEA_SSH_PORT}:${GITEA_SSH_PORT} -e INSTALL_LOCK=true -e SSH_PORT=${GITEA_SSH_PORT} -e GITEA__server__SSH_DOMAIN=$(GITEA_DOMAIN) -e GITEA__webhook__ALLOWED_HOST_LIST=* --name gitea gitea/gitea:$(GITEA_VERSION); \
  		echo "Starting gitea..."; \
  		sleep 5; \
  		docker exec gitea su-exec git /app/gitea/gitea admin user create --username $(GITEA_USER) --password $(GITEA_PASSWORD) --email $(GITEA_USER)@gitea.eu --config /data/gitea/conf/app.ini; \
		docker exec gitea su-exec git /app/gitea/gitea admin user create --username $(GITEA_USER_FORK) --password $(GITEA_PASSWORD) --email $(GITEA_USER_FORK)@gitea.eu --must-change-password=false --config /data/gitea/conf/app.ini; \
		echo "Gitea started"; \
  	fi

test-docker-compose:
	(cd ../tools/smtpmock/ && docker build -t smtpmock . && docker run -d -p 2023:2023 -p 2024:2024 smtpmock) && \
	docker compose up --no-recreate -d cds-db cds-cache elasticsearch && \
	sleep 5	&& \
	docker compose up --no-recreate cds-db-init && \
	docker compose up --no-recreate cds-migrate && \
	docker compose up cds-prepare && \
	CDS_EDIT_CONFIG="api.smtp.disable=false api.smtp.port=2023 api.smtp.host=$(HOSTNAME) " docker compose up cds-edit-config && \
	docker compose up -d cds-api && \
	echo "waiting 8s to let cds-api start.." && sleep 8 && \
	docker ps && \
	echo "downloading cdsctl from http://localhost:8081/download/cdsctl/$(UNAME_S)/amd64?variant=nokeychain -o cdsctl" && \
	curl http://localhost:8081/download/cdsctl/$(UNAME_S)/amd64?variant=nokeychain -o cdsctl && \
	chmod +x cdsctl && \
	cdsctl version && \
	export CDSCTL=`realpath ./cdsctl` && \
	TOKEN_CMD=`docker logs cds-cds-prepare-1|grep TOKEN` && \
	$$TOKEN_CMD && \
	./test.sh smoke_api initialization && \
	CDS_EDIT_CONFIG="hatchery.swarm.commonConfiguration.api.tokenV2=$(cat tmp.hatcheryv2.token)" docker compose up cds-edit-config && \
	docker compose up -d cds-cdn cds-ui cds-hooks cds-vcs cds-hatchery-local cds-elasticsearch && \
	echo "waiting 5s to let services start.." && sleep 5 && \
	./test.sh smoke_services cli;

merge-coverage:
	@docker run -v `pwd`:/workspace golang:1.24-alpine sh -c "\
			go install github.com/wadey/gocovmerge@latest && \
			cd /workspace && \
      gocovmerge $(COVER_FILES) > /workspace/cdsctl.cover.out \
		"