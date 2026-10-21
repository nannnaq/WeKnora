"""Remove only temporary resources created by this deployment before isolation was clarified."""
import json
from pathlib import Path
import runpy
import subprocess

ROOT = Path(__file__).resolve().parents[2]
os_state = ROOT / '.weknora-deploy/migration'
helpers = runpy.run_path(str(ROOT / 'deploy/local152/migrate.py'), run_name='migration_helpers')
resources = json.loads((os_state / 'temporary-shared-resources.json').read_text())
assert resources['db'] == resources['role'] == 'weknora_152'
assert resources['bucket'] == 'weknora'
original = json.loads((os_state / 'db-imported.json').read_text())
standalone = json.loads((os_state / 'db-imported-standalone.json').read_text())
assert original == standalone
assert '402 objects' in (ROOT / '.weknora-deploy/logs/object-import-standalone.log').read_text()
assert helpers['sql']('SELECT count(*) FROM knowledges') == str(standalone['knowledges'])


def shared_sql(query, database='postgres'):
    return subprocess.run(
        ['docker', 'exec', '-i', 'postgres-16.4-with-timescale', 'sh', '-c',
         'psql -X -At -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d ' + database],
        input=query, text=True, capture_output=True, check=True).stdout.strip()


if shared_sql("SELECT datname FROM pg_database WHERE datname='weknora_152'"):
    # This database and role did not exist before this deployment; all rows
    # must still match our own import manifest before they can be removed.
    for table, count in original.items():
        assert shared_sql('SELECT count(*) FROM public."' + table + '"', 'weknora_152') == str(count)
    assert shared_sql("SELECT count(*) FROM pg_stat_activity WHERE datname='weknora_152' AND backend_type NOT IN ('TimescaleDB Background Worker Scheduler')") == '0'
    shared_sql('DROP DATABASE weknora_152 WITH (FORCE)')
    shared_sql('DROP ROLE weknora_152')
    print('Removed this deployment temporary shared database and role')

store = helpers['s3']('http://192.168.18.152:7000', resources['minio_user'], resources['minio_password'])
if 'weknora' in [x['Name'] for x in store.list_buckets()['Buckets']]:
    expected = {x['key']: x['size'] for x in json.loads((os_state / 'objects.json').read_text())}
    actual = {}
    for page in store.get_paginator('list_objects_v2').paginate(Bucket='weknora'):
        actual.update({x['Key']: x['Size'] for x in page.get('Contents', [])})
    assert actual == expected, 'Unexpected objects; refusing cleanup'
    response = store.delete_objects(Bucket='weknora', Delete={'Objects': [{'Key': key} for key in expected]})
    assert not response.get('Errors')
    store.delete_bucket(Bucket='weknora')
    print('Removed this deployment temporary shared MinIO bucket')
(os_state / 'temporary-shared-cleaned.json').write_text(json.dumps({'database': 'weknora_152', 'role': 'weknora_152', 'bucket': 'weknora', 'other_resources_changed': False}))
(os_state / 'temporary-shared-resources.json').unlink()
