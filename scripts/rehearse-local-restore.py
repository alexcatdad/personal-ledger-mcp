#!/usr/bin/env python3
"""Verify a logical restore of the fixed, disposable local demo database."""

import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import uuid


CONTAINER = "pa-mcp-dev-postgres"
SOURCE = "pa_mcp_demo"
USER = "pa_mcp"


def command(*args, **kwargs):
    return subprocess.run(
        ["docker", "exec", "-i", CONTAINER, *args],
        check=True, stderr=subprocess.PIPE, **kwargs,
    )


def query(database, sql):
    return command(
        "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", USER,
        "-d", database, "-c", sql, stdout=subprocess.PIPE,
    ).stdout.decode().splitlines()


def identifier(value):
    return '"' + value.replace('"', '""') + '"'


def manifest(database):
    tables = {}
    for table in query(database, "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename"):
        rows = query(database, f"SELECT row_to_json(t)::jsonb::text FROM public.{identifier(table)} t")
        encoded = json.dumps(sorted(rows), ensure_ascii=False).encode()
        tables[table] = {"rows": len(rows), "sha256": hashlib.sha256(encoded).hexdigest()}
    sequences = {}
    for sequence in query(database, "SELECT sequencename FROM pg_sequences WHERE schemaname='public' ORDER BY sequencename"):
        sequences[sequence] = query(database, f"SELECT last_value,is_called FROM public.{identifier(sequence)}")
    return {"tables": tables, "sequences": sequences}


def main():
    # No target/host override: this helper must never point at a shared database.
    os.umask(0o077)
    output = Path(tempfile.mkdtemp(prefix="pa-mcp-restore-"))
    target = "pa_mcp_recovery_" + uuid.uuid4().hex
    created = False
    evidence = {"source": SOURCE, "target": target, "status": "started"}
    try:
        before = manifest(SOURCE)
        if not before["tables"]:
            raise RuntimeError("Source has no public tables; refusing a vacuous restore check")
        with (output / "database.dump").open("wb") as dump:
            command("pg_dump", "-U", USER, "-d", SOURCE, "-Fc", stdout=dump)
        command("createdb", "-U", USER, target, stdout=subprocess.PIPE)
        created = True
        with (output / "database.dump").open("rb") as dump:
            command("pg_restore", "-U", USER, "-d", target, "--exit-on-error",
                    "--no-owner", "--no-acl", stdin=dump, stdout=subprocess.PIPE)
        restored = manifest(target)
        after = manifest(SOURCE)
        evidence.update(source_manifest=before, restored_manifest=restored)
        if before != after:
            raise RuntimeError("Source changed during rehearsal; retry while demo writes are stopped")
        if restored != before:
            raise RuntimeError("Restored table contents or sequence state differ from source")
        evidence["status"] = "passed"
    except Exception as error:
        evidence.update(status="failed", error=str(error))
        raise
    finally:
        try:
            if created:
                command("dropdb", "-U", USER, target, stdout=subprocess.PIPE)
                evidence["recovery_database_removed"] = True
        finally:
            (output / "evidence.json").write_text(json.dumps(evidence, indent=2) + "\n")
            print(f"Evidence and retained dump: {output}")
    print(f"Verified {len(before['tables'])} tables and {len(before['sequences'])} sequences")


if __name__ == "__main__":
    main()
