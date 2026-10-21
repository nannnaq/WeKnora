"""Read-only export from 103; imports touch only WeKnora resources on 152."""
import argparse
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import time
import socketserver
import select
import threading
from contextlib import contextmanager

ROOT = Path(__file__).resolve().parents[2]
STATE = ROOT / '.weknora-deploy/migration'
sys.path.insert(0, str(ROOT / '.weknora-deploy/pydeps'))
import boto3
from botocore.config import Config
import paramiko
import requests

os.umask(0o077)
STATE.mkdir(parents=True, exist_ok=True)


def source():
    client = paramiko.SSHClient()
    client.load_system_host_keys()
    client.connect('192.168.18.103', username='casia',
                   password=os.environ['WEKNORA_SOURCE_PASSWORD'],
                   allow_agent=False, look_for_keys=False, timeout=10)
    return client


def remote(client, command):
    _, out, err = client.exec_command(command, timeout=60)
    data = out.read()
    errors = err.read().decode()
    if out.channel.recv_exit_status():
        raise RuntimeError(errors)
    return data


def envfile(path):
    return dict(line.split('=', 1) for line in Path(path).read_text().splitlines()
                if '=' in line and not line.lstrip().startswith('#'))


def s3(endpoint, user, password):
    return boto3.client('s3', endpoint_url=endpoint, aws_access_key_id=user,
                        aws_secret_access_key=password,
                        config=Config(signature_version='s3v4',
                                      s3={'addressing_style': 'path'}))


@contextmanager
def tunnel(client, port, host='127.0.0.1'):
    class Handler(socketserver.BaseRequestHandler):
        def handle(self):
            channel = client.get_transport().open_channel(
                'direct-tcpip', (host, port), self.request.getpeername())
            try:
                while True:
                    readable, _, _ = select.select([self.request, channel], [], [], 30)
                    for conn in readable:
                        data = conn.recv(65536)
                        if not data:
                            return
                        (channel if conn is self.request else self.request).sendall(data)
            finally:
                channel.close()
    server = socketserver.ThreadingTCPServer(('127.0.0.1', 0), Handler)
    server.daemon_threads = True
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        yield server.server_address[1]
    finally:
        server.shutdown()
        server.server_close()


def export_graph():
    from neo4j import GraphDatabase, Query
    client = source()
    env = json.loads((STATE / 'source-secrets.json').read_text())
    kb_ids = remote(client, "docker exec WeKnora-postgres psql -U postgres -d WeKnora -Atc 'select id from knowledge_bases'").decode().splitlines()
    knowledge_ids = remote(client, "docker exec WeKnora-postgres psql -U postgres -d WeKnora -Atc 'select id from knowledges'").decode().splitlines()
    scope = {'labels': ['ENTITY' + kb.replace('-', '_') for kb in kb_ids],
             'knowledge_ids': knowledge_ids}
    (STATE / 'graph-scope.json').write_text(json.dumps(scope))
    with tunnel(client, 9756) as port:
        with GraphDatabase.driver(f'bolt://127.0.0.1:{port}',
                                  auth=('neo4j', env['NEO4J_PASSWORD']),
                                  connection_timeout=15,
                                  connection_acquisition_timeout=20) as driver:
            with driver.session(database='neo4j', default_access_mode='READ') as session:
                queries = {
                    'nodes': 'MATCH (n) WHERE any(label IN labels(n) WHERE label IN $labels) AND n.kg IN $knowledge_ids RETURN elementId(n) AS id, labels(n) AS labels, properties(n) AS props',
                    'relationships': 'MATCH (a)-[r]->(b) WHERE any(label IN labels(a) WHERE label IN $labels) AND a.kg IN $knowledge_ids AND any(label IN labels(b) WHERE label IN $labels) AND b.kg IN $knowledge_ids RETURN elementId(r) AS id, elementId(a) AS start, elementId(b) AS end, type(r) AS type, properties(r) AS props',
                }
                for name, query in queries.items():
                    count = 0
                    with (STATE / ('graph-' + name + '.jsonl')).open('w') as f:
                        for record in session.run(Query(query, timeout=45), scope):
                            f.write(json.dumps(record.data(), ensure_ascii=False) + '\n')
                            count += 1
                    print('Neo4j exported', name, count, flush=True)
    client.close()


def export():
    client = source()
    with client.open_sftp() as ftp:
        text = ftp.open('/mnt/local4t/cjy/WeKnora/.env').read().decode()
    env = dict(line.split('=', 1) for line in text.splitlines()
               if '=' in line and not line.lstrip().startswith('#'))
    selected = {k: v for k, v in env.items() if k in {
        'SYSTEM_AES_KEY', 'MINIO_ACCESS_KEY_ID', 'MINIO_SECRET_ACCESS_KEY',
        'MINIO_BUCKET_NAME', 'NEO4J_USERNAME', 'NEO4J_PASSWORD'}}
    (STATE / 'source-secrets.json').write_text(json.dumps(selected))
    dump = STATE / 'postgres.sql'
    if not dump.exists():
        command = ('docker exec WeKnora-postgres pg_dump -U postgres -d WeKnora '
                   '--schema=public --exclude-table=public.spatial_ref_sys '
                   '--no-owner --no-privileges')
        _, out, err = client.exec_command(command, timeout=120)
        with dump.with_suffix('.partial').open('wb') as f:
            while data := out.read(1024 * 1024):
                f.write(data)
        errors = err.read().decode()
        if out.channel.recv_exit_status():
            raise RuntimeError(errors)
        dump.with_suffix('.partial').rename(dump)
    print('PostgreSQL exported', dump.stat().st_size, flush=True)
    client.close()

    store = s3('http://192.168.18.103:9753', env['MINIO_ACCESS_KEY_ID'],
               env['MINIO_SECRET_ACCESS_KEY'])
    objects = []
    for page in store.get_paginator('list_objects_v2').paginate(Bucket=env['MINIO_BUCKET_NAME']):
        for item in page.get('Contents', []):
            key = item['Key']
            local = STATE / 'objects' / key
            if '..' in Path(key).parts or key.startswith('/'):
                raise RuntimeError('Unsafe object key')
            local.parent.mkdir(parents=True, exist_ok=True)
            if not local.exists() or local.stat().st_size != item['Size']:
                store.download_file(env['MINIO_BUCKET_NAME'], key, str(local))
            meta = store.head_object(Bucket=env['MINIO_BUCKET_NAME'], Key=key)
            objects.append({'key': key, 'size': item['Size'], 'etag': item['ETag'],
                            'content_type': meta.get('ContentType', 'application/octet-stream'),
                            'metadata': meta.get('Metadata', {})})
    (STATE / 'objects.json').write_text(json.dumps(objects, ensure_ascii=False))
    print('MinIO exported', len(objects), 'objects', sum(x['size'] for x in objects), 'bytes', flush=True)

    url = 'http://192.168.18.103:9757'
    collections = requests.get(url + '/collections', timeout=30).json()['result']['collections']
    for entry in collections:
        name = entry['name']
        if not name.startswith('weknora_'):
            continue
        info = requests.get(url + '/collections/' + name, timeout=30).json()['result']
        (STATE / (name + '.config.json')).write_text(json.dumps(info))
        count = 0
        with (STATE / (name + '.jsonl')).open('w') as f:
            offset = None
            while True:
                body = {'limit': 100, 'with_payload': True, 'with_vector': True}
                if offset is not None:
                    body['offset'] = offset
                r = requests.post(url + '/collections/' + name + '/points/scroll', json=body, timeout=60)
                r.raise_for_status()
                result = r.json()['result']
                for point in result['points']:
                    f.write(json.dumps(point, ensure_ascii=False) + '\n')
                    count += 1
                offset = result.get('next_page_offset')
                if offset is None:
                    break
        if count != info['points_count']:
            raise RuntimeError(f'Collection changed during read: {name}')
        print('Qdrant exported', name, count, 'points', flush=True)


def prepare():
    src = json.loads((STATE / 'source-secrets.json').read_text())
    import secrets
    env = {
        'GIN_MODE': 'release', 'TZ': 'Asia/Shanghai', 'WEKNORA_LANGUAGE': 'zh-CN',
        'LOG_LEVEL': 'info', 'AUTO_MIGRATE': 'true',
        'DB_DRIVER': 'postgres', 'DB_HOST': 'postgres', 'DB_PORT': '5432',
        'DB_USER': 'weknora_152', 'DB_NAME': 'weknora_152', 'DB_PASSWORD': secrets.token_hex(24),
        'RETRIEVE_DRIVER': 'qdrant', 'QDRANT_HOST': '192.168.18.152', 'QDRANT_PORT': '9758',
        'QDRANT_COLLECTION': 'weknora_embeddings', 'QDRANT_USE_TLS': 'false',
        'DOCREADER_ADDR': 'docreader:50051', 'DOCREADER_TRANSPORT': 'grpc',
        'STORAGE_TYPE': 'minio', 'MINIO_ENDPOINT': '192.168.18.152:9753',
        'MINIO_ACCESS_KEY_ID': 'weknora152',
        'MINIO_SECRET_ACCESS_KEY': secrets.token_hex(24),
        'MINIO_BUCKET_NAME': 'weknora', 'MINIO_USE_SSL': 'false',
        'LOCAL_STORAGE_BASE_DIR': '/data/files',
        'STREAM_MANAGER_TYPE': 'redis', 'REDIS_ADDR': 'redis:6379',
        'REDIS_PASSWORD': secrets.token_hex(24), 'REDIS_DB': '0', 'REDIS_PREFIX': 'weknora152:stream:',
        'WEKNORA_REDIS_NAMESPACE': 'weknora152',
        'NEO4J_ENABLE': 'true', 'NEO4J_URI': 'bolt://192.168.18.152:9756',
        'NEO4J_USERNAME': 'neo4j', 'NEO4J_PASSWORD': secrets.token_hex(24),
        'SYSTEM_AES_KEY': src['SYSTEM_AES_KEY'], 'JWT_SECRET': secrets.token_hex(32),
        'DISABLE_REGISTRATION': 'false', 'LANGFUSE_ENABLED': 'false',
        'MCP_SERVER_AUTH_TOKEN': secrets.token_hex(32), 'WEKNORA_API_KEY': '',
        'OLLAMA_OPTIONAL': 'true', 'OLLAMA_BASE_URL': 'http://192.168.18.152:11434',
        'APP_EXTERNAL_URL': 'http://192.168.18.152:9750',
        'FRONTEND_BASE_URL': 'http://192.168.18.152:9750',
        'SSRF_WHITELIST_EXTRA': '192.168.18.152,docreader,searxng,qdrant,neo4j',
        'REALTIME_CONTEXT_ENABLED': 'true',
        'REALTIME_CONTEXT_GRAPH_BASE_URL': 'http://192.168.18.152:9707',
        'REALTIME_CONTEXT_RULE_BASE_URL': 'http://192.168.18.152:9701',
        'REALTIME_CONTEXT_CACHE_TTL': '60s', 'REALTIME_CONTEXT_REQUEST_TIMEOUT': '5s',
        'REALTIME_CONTEXT_MAX_MATCHED_ENTITIES': '20', 'REALTIME_CONTEXT_MAX_CONTEXT_CHARS': '32000',
    }
    path = ROOT / '.env'
    if path.exists():
        raise RuntimeError('Refusing to replace existing .env')
    path.write_text('\n'.join(f'{k}={v}' for k, v in env.items()) + '\n')
    path.chmod(0o600)
    for directory in ['files', 'docreader-tmp', 'qdrant', 'neo4j']:
        (ROOT / '.weknora-deploy/runtime' / directory).mkdir(parents=True, exist_ok=True)
    (ROOT / 'deploy/local152/searxng-settings.yml').write_text(
        'use_default_settings: true\nserver:\n  secret_key: ' + secrets.token_hex(32)
        + '\n  limiter: false\nsearch:\n  formats: [html, json]\n')
    print('Local configuration prepared; source encryption key preserved', flush=True)


def sql(query, db='weknora_152'):
    result = subprocess.run(
        ['docker', 'exec', '-i', 'WeKnora-postgres', 'sh', '-c',
         'psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d ' + shlex.quote(db) + ' -At'],
        input=query, text=True, capture_output=True, check=True)
    return result.stdout.strip()


def quote(text):
    return "'" + str(text).replace("'", "''") + "'"


def import_db():
    env = envfile(ROOT / '.env')
    if (STATE / 'db-imported-standalone.json').exists():
        print('Database already imported', flush=True)
        return
    if int(sql("SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")):
        raise RuntimeError('Destination project database is not empty; refusing to overwrite')
    sql('CREATE EXTENSION IF NOT EXISTS "uuid-ossp"; CREATE EXTENSION IF NOT EXISTS pg_trgm;')
    expected = {}
    table = None
    compatible = STATE / 'postgres-pg16.sql'
    with compatible.open('w') as dest:
        dest.write('SET ROLE weknora_152;\n')
        for line in (STATE / 'postgres.sql').open():
            if line.startswith(('\\restrict ', '\\unrestrict ', 'SET transaction_timeout')):
                continue
            if line == 'CREATE SCHEMA public;\n':
                line = 'CREATE SCHEMA IF NOT EXISTS public;\n'
            if line.startswith('COPY public.'):
                table = line.split()[1].split('.', 1)[1]
                expected[table] = 0
            elif table is not None:
                if line.rstrip() == '\\.':
                    table = None
                else:
                    expected[table] += 1
            dest.write(line)
    command = ['docker', 'exec', '-i', 'WeKnora-postgres', 'sh', '-c',
               'psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d weknora_152']
    with compatible.open('rb') as data, (STATE / 'postgres-restore.log').open('w') as log:
        result = subprocess.run(command, stdin=data, stdout=log, stderr=log)
    if result.returncode:
        raise RuntimeError('Restore failed; inspect migration/postgres-restore.log')
    actual = {}
    for table, count in expected.items():
        actual[table] = int(sql('SELECT count(*) FROM public."' + table + '"'))
        if actual[table] != count:
            raise RuntimeError(f'Row count mismatch: {table}, {count}, {actual[table]}')
    (STATE / 'db-imported-standalone.json').write_text(json.dumps(actual, indent=2))
    print('PostgreSQL imported and verified', len(actual), 'project tables', flush=True)


def configure_db():
    import yaml
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM
    import base64
    env = envfile(ROOT / '.env')
    def encrypt(value):
        nonce = os.urandom(12)
        cipher = AESGCM(env['SYSTEM_AES_KEY'].encode()).encrypt(nonce, value.encode(), None)
        return 'enc:v1:' + base64.urlsafe_b64encode(nonce + cipher).decode().rstrip('=')
    builtin = yaml.safe_load((ROOT / 'config/builtin_models.yaml').read_text())['builtin_models']
    for model in builtin:
        params = json.dumps(model['parameters'])
        sql('UPDATE models SET name=' + quote(model['name']) + ', parameters=' + quote(params)
            + '::jsonb WHERE type=' + quote(model['type']))
    storage = {'mode': 'remote', 'endpoint': env['MINIO_ENDPOINT'],
               'bucket_name': env['MINIO_BUCKET_NAME'],
               'access_key_id': encrypt(env['MINIO_ACCESS_KEY_ID']),
               'secret_access_key': encrypt(env['MINIO_SECRET_ACCESS_KEY'])}
    sql('UPDATE storage_backends SET config=' + quote(json.dumps(storage))
        + "::jsonb WHERE provider='minio' AND source='env'")
    sql("UPDATE web_search_providers SET parameters=replace(parameters::text, 'searxng:8080', '192.168.18.152:9752')::jsonb WHERE provider='searxng'")
    # Preserve document contents; update only structured service URLs in metadata.
    fields = {
        'knowledge_bases': ['storage_provider_config', 'vlm_config', 'cos_config'],
        'custom_agents': ['config'], 'vector_stores': ['connection_config'],
        'mcp_services': ['url'], 'web_search_providers': ['parameters'],
        'resources': ['physical_path'], 'knowledges': ['file_path'],
    }
    for table, columns in fields.items():
        for column in columns:
            kind = sql("SELECT data_type FROM information_schema.columns WHERE table_schema='public' AND table_name=" + quote(table) + ' AND column_name=' + quote(column))
            if not kind:
                continue
            cast = '::jsonb' if kind == 'jsonb' else ('::json' if kind == 'json' else '')
            sql(f'UPDATE {table} SET {column}=replace({column}::text, \'192.168.18.103\', \'192.168.18.152\'){cast} WHERE {column}::text LIKE \'%192.168.18.103%\'')
    print('Local model endpoints and project storage credentials updated', flush=True)


def import_objects():
    env = envfile(ROOT / '.env')
    store = s3('http://' + env['MINIO_ENDPOINT'], env['MINIO_ACCESS_KEY_ID'], env['MINIO_SECRET_ACCESS_KEY'])
    bucket = env['MINIO_BUCKET_NAME']
    exists = bucket in [x['Name'] for x in store.list_buckets()['Buckets']]
    if exists and not (STATE / 'object-import-started').exists():
        raise RuntimeError('Destination bucket exists; refusing to overwrite')
    if not exists:
        store.create_bucket(Bucket=bucket)
    (STATE / 'object-import-started').touch()
    objects = json.loads((STATE / 'objects.json').read_text())
    import hashlib
    verified = 0
    for item in objects:
        with (STATE / 'objects' / item['key']).open('rb') as f:
            store.put_object(Bucket=bucket, Key=item['key'], Body=f,
                             ContentType=item['content_type'], Metadata=item['metadata'])
        source_hash = hashlib.sha256((STATE / 'objects' / item['key']).read_bytes()).digest()
        response = store.get_object(Bucket=bucket, Key=item['key'])
        checksum = hashlib.sha256()
        while data := response['Body'].read(1024 * 1024):
            checksum.update(data)
        if checksum.digest() != source_hash:
            raise RuntimeError('Object checksum mismatch: ' + item['key'])
        verified += 1
    (STATE / 'objects-verified.json').write_text(json.dumps({'count': verified, 'bytes': sum(x['size'] for x in objects), 'checksum': 'SHA-256'}))
    print('MinIO imported and SHA-256 verified', verified, 'objects', flush=True)


def import_vectors():
    url = 'http://192.168.18.152:9757'
    for path in STATE.glob('weknora_*.jsonl'):
        name = path.stem
        info = json.loads((STATE / (name + '.config.json')).read_text())
        params = info['config']['params']
        r = requests.put(url + '/collections/' + name, json={'vectors': params['vectors']}, timeout=60)
        if r.status_code not in [200, 409]:
            r.raise_for_status()
        for key, index in info.get('payload_schema', {}).items():
            r = requests.put(url + '/collections/' + name + '/index', json={'field_name': key, 'field_schema': index.get('params') or index['data_type']}, timeout=60)
            r.raise_for_status()
        fresh = 'weknora_embeddings_2560'
        r = requests.put(url + '/collections/' + fresh, json={'vectors': {'size': 2560, 'distance': 'Cosine'}}, timeout=60)
        if r.status_code not in [200, 409]:
            r.raise_for_status()
        for key, index in info.get('payload_schema', {}).items():
            r = requests.put(url + '/collections/' + fresh + '/index', json={'field_name': key, 'field_schema': index.get('params') or index['data_type']}, timeout=60)
            r.raise_for_status()
        count = 0
        checkpoint = STATE / 'vectors-2560.jsonl'
        existing = {json.loads(line)['id']: json.loads(line) for line in checkpoint.open()} if checkpoint.exists() else {}
        def batch(points):
            nonlocal count
            r = requests.put(url + '/collections/' + name + '/points?wait=true', json={'points': points}, timeout=60)
            r.raise_for_status()
            missing = [p for p in points if p['id'] not in existing]
            if missing:
                r = requests.post('http://192.168.18.152:8001/v1/embeddings',
                                  json={'model': '/workspace/model_embedding_w8a8',
                                        'input': [p['payload']['content'] or ' ' for p in missing]}, timeout=180)
                r.raise_for_status()
                values = sorted(r.json()['data'], key=lambda x: x['index'])
                assert len(values) == len(missing)
                with checkpoint.open('a') as f:
                    for point, value in zip(missing, values):
                        assert len(value['embedding']) == 2560
                        new = {'id': point['id'], 'payload': point['payload'], 'vector': value['embedding']}
                        existing[new['id']] = new
                        f.write(json.dumps(new, ensure_ascii=False) + '\n')
            r = requests.put(url + '/collections/' + fresh + '/points?wait=true',
                             json={'points': [existing[p['id']] for p in points]}, timeout=60)
            r.raise_for_status()
            count += len(points)
            print('Vectors imported and rebuilt', count, '/', info['points_count'], flush=True)
        pending = []
        for line in path.open():
            point = json.loads(line)
            pending.append({k: point[k] for k in ['id', 'payload', 'vector']})
            if len(pending) >= 32:
                batch(pending)
                pending = []
        if pending:
            batch(pending)
        for collection in [name, fresh]:
            actual = requests.get(url + '/collections/' + collection, timeout=30).json()['result']['points_count']
            if actual != info['points_count']:
                raise RuntimeError('Vector count mismatch: ' + collection)
        (STATE / 'vectors-verified.json').write_text(json.dumps({'source': name, 'active': fresh, 'count': count}))


def import_graph():
    from neo4j import GraphDatabase
    env = envfile(ROOT / '.env')
    nodes = [json.loads(x) for x in (STATE / 'graph-nodes.jsonl').open()]
    rels = [json.loads(x) for x in (STATE / 'graph-relationships.jsonl').open()]
    with GraphDatabase.driver(env['NEO4J_URI'], auth=('neo4j', env['NEO4J_PASSWORD'])) as driver:
        with driver.session(database='neo4j') as session:
            if session.run('MATCH (n) RETURN count(n) AS count').single()['count']:
                raise RuntimeError('Destination graph is not empty')
            for start in range(0, len(nodes), 200):
                session.run('UNWIND $rows AS row CALL apoc.create.node(row.labels, row.props) YIELD node SET node.__migration_id=row.id RETURN count(node)', rows=nodes[start:start+200]).consume()
            for start in range(0, len(rels), 200):
                session.run('UNWIND $rows AS row MATCH (a {__migration_id:row.start}), (b {__migration_id:row.end}) CALL apoc.create.relationship(a,row.type,row.props,b) YIELD rel RETURN count(rel)', rows=rels[start:start+200]).consume()
            actual_nodes = session.run('MATCH (n) RETURN count(n) AS count').single()['count']
            actual_rels = session.run('MATCH ()-[r]->() RETURN count(r) AS count').single()['count']
            assert (actual_nodes, actual_rels) == (len(nodes), len(rels))
            session.run('MATCH (n) WHERE n.__migration_id IS NOT NULL REMOVE n.__migration_id').consume()
    (STATE / 'graph-verified.json').write_text(json.dumps({'nodes': len(nodes), 'relationships': len(rels)}))
    print('Neo4j imported and verified', len(nodes), 'nodes', len(rels), 'relationships', flush=True)


def migrate_redis():
    import redis
    import base64
    def snapshot(connection, key, kind):
        encode = lambda value: base64.b64encode(value).decode()
        if kind == b'string':
            value = connection.get(key)
            return encode(value) if value is not None else None
        if kind == b'list':
            return [encode(x) for x in connection.lrange(key, 0, -1)]
        if kind == b'set':
            return sorted(encode(x) for x in connection.smembers(key))
        if kind == b'zset':
            return [[encode(x), score] for x, score in connection.zrange(key, 0, -1, withscores=True)]
        if kind == b'hash':
            return sorted([encode(k), encode(v)] for k, v in connection.hgetall(key).items())
        raise RuntimeError('Unhandled Redis key type')
    client = source()
    with client.open_sftp() as ftp:
        text = ftp.open('/mnt/local4t/cjy/WeKnora/.env').read().decode()
    src = dict(l.split('=', 1) for l in text.splitlines() if '=' in l and not l.startswith('#'))
    host = remote(client, "docker inspect WeKnora-redis --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'").decode().strip()
    exported = []
    with tunnel(client, 6379, host) as port:
        original = redis.Redis(host='127.0.0.1', port=port, password=src['REDIS_PASSWORD'], db=0, socket_timeout=30)
        for key in original.scan_iter(count=200):
            pipeline = original.pipeline(transaction=True)
            pipeline.dump(key)
            pipeline.pttl(key)
            dump, ttl = pipeline.execute()
            if dump is None:
                continue
            kind = original.type(key)
            exported.append({'key': base64.b64encode(key).decode(), 'dump': base64.b64encode(dump).decode(),
                             'expires_at': int(time.time() * 1000) + ttl if ttl >= 0 else None,
                             'kind': kind.decode(), 'value': snapshot(original, key, kind),
                             'runtime': key.startswith((b'asynq:servers', b'asynq:workers'))})
    (STATE / 'redis-export.json').write_text(json.dumps(exported))
    env = envfile(ROOT / '.env')
    host = json.loads(subprocess.check_output(['docker', 'inspect', 'WeKnora-redis']))[0]['NetworkSettings']['Networks']['weknora_default']['IPAddress']
    target = redis.Redis(host=host, password=env['REDIS_PASSWORD'], db=0, socket_timeout=30)
    restored = 0
    for item in exported:
        if item['runtime']:
            continue
        expiry = item['expires_at']
        if expiry is not None and expiry <= int(time.time() * 1000):
            continue
        key = base64.b64decode(item['key'])
        payload = base64.b64decode(item['dump'])
        ttl = expiry - int(time.time() * 1000) if expiry is not None else 0
        target.restore(key, ttl, payload, replace=True)
        if snapshot(target, key, item['kind'].encode()) != item['value']:
            raise RuntimeError('Redis value verification failed')
        restored += 1
    client.close()
    (STATE / 'redis-verified.json').write_text(json.dumps({'exported': len(exported), 'restored': restored, 'runtime_excluded': sum(x['runtime'] for x in exported)}))
    print('Redis project task history and statistics restored', restored, 'keys; source worker identities excluded', flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=['export', 'prepare', 'export_graph', 'import_db', 'configure_db', 'import_objects', 'import_vectors', 'import_graph', 'migrate_redis'])
    globals()[parser.parse_args().action]()
