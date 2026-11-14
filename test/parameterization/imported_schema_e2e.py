"""Imported constraints survive save, preview, freeze and native execution.

Synthetic namespace-local example only; no business or cloud resource actions.
"""
import copy
import json
from e2e import api, wait_workflow, NS, PREFIX, RUN
from template_refs_e2e import apply


def main():
    name = RUN + '-import'
    schema = {'type': 'object', 'required': ['answers'], 'properties': {
        'answers': {'type': 'array', 'minItems': 3, 'maxItems': 3,
                    'prefixItems': [{'type': 'string', 'const': 'reviewed'},
                                    {'type': 'string', 'format': 'ipv4'},
                                    {'type': 'string', 'format': 'ipv4-cidr'}], 'items': False},
        'count': {'type': 'integer', 'minimum': 0, 'maximum': 18446744073709552000,
                  'deprecated': True}}}
    group = 'parameters.workflows.example.io/v1alpha1'
    def definition(kind, spec):
        apply({'apiVersion': group, 'kind': kind, 'metadata': {'name': name, 'namespace': NS}, 'spec': spec})
    definition('ParameterTemplate', {'dialect': 'http://json-schema.org/draft-07/schema#', 'schema': schema})
    apply({'apiVersion': 'argoproj.io/v1alpha1', 'kind': 'WorkflowTemplate',
           'metadata': {'name': name, 'namespace': NS,
                        'labels': {'workflows.argoproj.io/controller-instanceid': 'parameterization-lab'}},
           'spec': {'entrypoint': 'echo', 'serviceAccountName': 'workflow-executor',
                    'arguments': {'parameters': [{'name': 'answers'}]},
                    'templates': [{'name': 'echo', 'inputs': {'parameters': [{'name': 'answers'}]},
                                   'container': {'image': 'alpine:3.20', 'command': ['sh', '-c'],
                                                 'args': ['printf "%s" "$ANSWERS"'],
                                                 'env': [{'name': 'ANSWERS', 'value': '{{inputs.parameters.answers}}'}]},
                                   'outputs': {'parameters': [{'name': 'echo',
                                                              'value': '{{inputs.parameters.answers}}'}]}}]}})
    definition('WorkflowParameterBinding', {'workflowTemplate': name, 'parameterTemplate': name,
               'mappings': [{'path': '/answers', 'parameter': 'answers', 'encoding': 'json'}]})
    values = {'answers': ['reviewed', '10.0.0.1', '10.1.0.0/24'], 'count': 0}
    revision = api('PUT', PREFIX + '/sets/' + name,
                   {'template': name, 'values': values, 'resourceVersion': ''})
    current = revision
    for invalid in [['wrong', '10.0.0.1', '10.1.0.0/24'],
                    ['reviewed', '10.0.0.999', '10.1.0.0/24'],
                    ['reviewed', '10.0.0.1', '10.1.0.0/33']]:
        bad = copy.deepcopy(values)
        bad['answers'] = invalid
        current = api('PUT', PREFIX + '/sets/' + name,
                      {'template': name, 'values': bad, 'resourceVersion': current['metadata']['resourceVersion']})
        preview = api('POST', PREFIX + '/preview', {'binding': name, 'revision': current['spec']['revision']})
        assert preview['errors'], 'invalid imported constraint must stop preview'
    preview = api('POST', PREFIX + '/preview', {'binding': name, 'revision': revision['spec']['revision']})
    assert not preview['errors'], preview
    workflow = api('POST', PREFIX + '/submit', {'binding': name, 'revision': revision['spec']['revision'],
                   'executionDigest': preview['executionDigest'], 'idempotencyKey': name})
    done = wait_workflow(workflow['metadata']['name'], lambda w: w['status']['phase'] == 'Succeeded')
    outputs = [p['value'] for n in done['status']['nodes'].values()
               for p in n.get('outputs', {}).get('parameters', []) if p['name'] == 'echo']
    assert outputs and all(json.loads(v) == values['answers'] for v in outputs)
    stored = api('GET', PREFIX + '/catalog/parametertemplates?name=' + name)
    assert stored['spec']['schema'] == schema, 'published source must not be lowered or rewritten'
    print('PASS imported tuple/IP/CIDR constraints: rejected invalid drafts, preserved source, native outputs unchanged')


if __name__ == '__main__':
    main()
