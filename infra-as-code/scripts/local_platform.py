# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The platform dependencies both local lanes run with the same pinned images and configuration.

Kind runs them in the cluster through the chart (templates/local-dependencies.yaml and dex.yaml); Native runs them with
Docker Compose (native/compose.yaml). In both the services are named temporal, minio, postgres and keycloak, platform Dex
uses the best-effort memory Stream store, MinIO holds the versioned blob and artifact buckets, PostgreSQL the server's
database, whose read model platform Dex writes, and Keycloak is the OAuth provider of the connection tests.
`make -C infra-as-code check` compares the chart's arguments with the Compose file's.
"""
import json
import os
import secrets
import subprocess

from commands import run
from operator_errors import require
from paths import CHART
from pins import names_for
from private_files import retained_secret, write_private

# The versioned buckets of the platform MinIO: platform Dex's blob store and the Release artifacts.
BUCKETS = ("superverse-v2-blobs", "superverse-v2-artifacts")
# The server's database, which the PostgreSQL image creates on its first start (cmd/superverse-server/config: postgres.database).
POSTGRES_DATABASE = "superverse"
# The superuser of the image and the roles of internal/comm/platform/postgres: the schema owner, which migrations connect as, the
# server, and platform Dex's Attribute Store, the only writer of the read model.
POSTGRES_SUPERUSER, POSTGRES_OWNER, POSTGRES_SERVER, POSTGRES_DEX = "postgres", "superverse_owner", "superverse_server", "superverse_dex"
POSTGRES_ROLES = (POSTGRES_SUPERUSER, POSTGRES_OWNER, POSTGRES_SERVER, POSTGRES_DEX)
POSTGRES_CREDENTIALS = "postgres-credentials.json"
# The read model's platform Dex Attribute Stores (docs/architecture/teamspace/read-model.md), with the names of the server's
# postgres.AttributeStore values. Each writes the synced Attributes of one Flow type into the row of its table that the row
# key splits from the FlowID, under snake_case column names; ProjectFlow's and ProjectSetupFlow's stores write different
# columns of one projects row. A store's attributeMaps write each instance of a map as a child row: the builds of a
# Release, and the members of a TeamSpace and of a Project.
READ_MODEL_STORES = {
    "team_spaces": {"tableName": "public.team_spaces", "rowKey": "teamspace-{team_space_id}",
                    "attributeMaps": {"Members": {"tableName": "public.team_space_members", "instanceKeyColumn": "account_id"}}},
    "projects": {"tableName": "public.projects", "rowKey": "project-{project_id}",
                 "attributeMaps": {"Members": {"tableName": "public.project_members", "instanceKeyColumn": "account_id"}}},
    "project_setups": {"tableName": "public.projects", "rowKey": "setup-{project_id}-{*}"},
    "releases": {"tableName": "public.releases", "rowKey": "release-{project_id}-{release_id}",
                 "attributeMaps": {"ReleaseBuildRevisions": {"tableName": "public.release_build_revisions", "instanceKeyColumn": "build_revision_id"}}},
    "deployments": {"tableName": "public.deployments", "rowKey": "deploy-{project_id}-{deployment_id}"},
}

# Keycloak in development mode imports this realm on every start, filling its placeholders from the container's
# environment: the client secret and the user's password, which the lane retains with the bootstrap administrator's
# password in its private keycloak-credentials.json, where the connection tests read them.
KEYCLOAK_REALM_FILE = CHART / "files/keycloak-realm.json"
KEYCLOAK_CREDENTIALS = "keycloak-credentials.json"


def dex_config(credentials, postgres=None, stores=None):
    """Platform Dex on the local Temporal and MinIO services, with the best-effort memory Stream store.

    With the lane's PostgreSQL role passwords it also writes the read model (`attribute_store`). Dex refuses to start while a
    table of its Attribute Store is missing, so the lanes pass them only once the server's migrations succeeded. `stores`, by
    default the checkout's READ_MODEL_STORES, are the store definitions to render.
    """
    config = {"log": {"stdout": True, "level": "info"}, "api": {"port": 8801},
              "interpreter": {"attributeIndexesManagedExternally": True, "temporal": {"hostPort": "temporal:7233", "namespace": names_for("test")["temporal_namespace"]}},
              "blobStore": {"enabled": True, "asyncStepInputSnapshotsEnabled": True, "thresholdInBytes": 100, "objectIdLength": 10,
                            "supportedStorages": [{"status": "active", "storageId": "p0", "storageType": "s3", "s3Endpoint": "http://minio:9000",
                                                   "s3Bucket": BUCKETS[0], "s3Region": "us-east-1", "s3AccessKey": credentials["access"],
                                                   "s3SecretKey": credentials["secret"],
                                                   "cleanupStrategy": {"cleanupStrategyType": "afterAllRunsDeleted", "cleanupFrequencyInDays": 1}}]},
              "streamStore": {"backend": "memory"},
              "matching": {"maxConcurrentTasksCap": 4224, "maxStreams": 100000}}
    if postgres is not None:
        config["attributeStore"] = attribute_store(postgres, stores)
    return config


def attribute_store(postgres, stores=None):
    """Platform Dex's Attribute Store of the read model, as the superverse_dex role, with a 168-hour sync retry budget.

    The budget lets the read model catch up after a long PostgreSQL outage, at the cost of a closing Flow waiting for its
    sync queue to drain.
    """
    dsn = f"postgres://{POSTGRES_DEX}:{postgres[POSTGRES_DEX]}@postgres:5432/{POSTGRES_DATABASE}?sslmode=disable"
    stores = READ_MODEL_STORES if stores is None else stores
    return {"stores": {name: {"type": "postgres", "dsn": dsn, "columnNaming": "snake_case", **store} for name, store in stores.items()},
            "syncRetryPolicy": {"totalDuration": "168h"}}


def postgres_credentials(state):
    """The lane's PostgreSQL role passwords, keyed by role name, retained in its private state directory; tests read the same file.

    A file written before a role existed gains a password for it; a retained password never changes.
    """
    path = state / POSTGRES_CREDENTIALS
    credentials = json.loads(retained_secret(path, lambda: json.dumps({role: secrets.token_urlsafe(32) for role in POSTGRES_ROLES})))
    missing = [role for role in POSTGRES_ROLES if role not in credentials]
    if missing:
        credentials.update({role: secrets.token_urlsafe(32) for role in missing})
        write_private(path, json.dumps(credentials))
    return credentials


def postgres_roles_sql(credentials):
    """Create the roles that are missing and set every password from the retained credentials, on every start.

    It runs as the superuser through the container's socket, so it works whatever password the data volume was created
    with. The owner owns the database and its public schema, where migrations create the tables and grant the server and
    Dex their privileges; the server and Dex only connect. Every role may log in, also one created earlier without LOGIN.
    The passwords are URL-safe tokens, so they need no quoting; the script reaches psql on stdin and is never printed.
    """
    statements = ["DO $$ BEGIN"]
    for role in POSTGRES_ROLES[1:]:
        statements.append(f"IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '{role}') THEN CREATE ROLE {role} LOGIN; END IF;")
    statements.append("END $$;")
    for role in POSTGRES_ROLES:
        password = credentials[role]
        require(password.replace("-", "").replace("_", "").isalnum(), "PostgreSQL passwords must be URL-safe tokens")
        statements.append(f"ALTER ROLE {role} LOGIN PASSWORD '{password}';")
    users = f"{POSTGRES_SERVER}, {POSTGRES_DEX}"
    statements += [f"ALTER DATABASE {POSTGRES_DATABASE} OWNER TO {POSTGRES_OWNER};", f"ALTER SCHEMA public OWNER TO {POSTGRES_OWNER};",
                   f"GRANT CONNECT ON DATABASE {POSTGRES_DATABASE} TO {users};", f"GRANT USAGE ON SCHEMA public TO {users};"]
    return "\n".join(statements) + "\n"


def keycloak_credentials(state):
    """The lane's Keycloak secrets, retained in its private state directory; a retained secret never changes."""
    return json.loads(retained_secret(state / KEYCLOAK_CREDENTIALS,
                                      lambda: json.dumps({name: secrets.token_urlsafe(32) for name in ("adminPassword", "clientSecret", "userPassword")})))


def keycloak_secret_environment(credentials):
    """The secret environment of the Keycloak container: the bootstrap administrator's password and the realm's secrets."""
    return {"KC_BOOTSTRAP_ADMIN_PASSWORD": credentials["adminPassword"], "SV2_KEYCLOAK_CLIENT_SECRET": credentials["clientSecret"],
            "SV2_KEYCLOAK_USER_PASSWORD": credentials["userPassword"]}


def create_buckets(endpoint, credentials):
    """Create the versioned buckets at the platform MinIO's `endpoint` on this machine, keeping existing ones."""
    env = dict(os.environ, AWS_ACCESS_KEY_ID=credentials["access"], AWS_SECRET_ACCESS_KEY=credentials["secret"],
               AWS_DEFAULT_REGION="us-east-1", AWS_EC2_METADATA_DISABLED="true")
    env.pop("AWS_SESSION_TOKEN", None)
    env.pop("AWS_PROFILE", None)
    s3api = ["aws", "--endpoint-url", endpoint, "--no-cli-pager", "s3api"]
    for bucket in BUCKETS:
        try:
            run([*s3api, "head-bucket", "--bucket", bucket], env=env, capture=True)
        except subprocess.CalledProcessError:
            run([*s3api, "create-bucket", "--bucket", bucket], env=env, capture=True)
        run([*s3api, "put-bucket-versioning", "--bucket", bucket, "--versioning-configuration", "Status=Enabled"], env=env, capture=True)
        versioning = json.loads(run([*s3api, "get-bucket-versioning", "--bucket", bucket], env=env, capture=True))
        require(versioning.get("Status") == "Enabled", "Versioning is not enabled: " + bucket)
