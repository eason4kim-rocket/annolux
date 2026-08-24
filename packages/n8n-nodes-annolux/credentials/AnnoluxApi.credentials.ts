import type {
	IAuthenticateGeneric,
	ICredentialTestRequest,
	ICredentialType,
	INodeProperties,
} from 'n8n-workflow';

export class AnnoluxApi implements ICredentialType {
	name = 'annoluxApi';
	displayName = 'Annolux API';
	documentationUrl = 'https://annolux.com/docs';

	properties: INodeProperties[] = [
		{
			displayName: 'API Key',
			name: 'apiKey',
			type: 'string',
			typeOptions: { password: true },
			default: '',
			required: true,
			description: 'Your Annolux live or sandbox API key (starts with ann_live_ or ann_test_)',
		},
		{
			displayName: 'API Base URL',
			name: 'baseUrl',
			type: 'string',
			default: 'https://api.annolux.com',
			required: false,
			description: 'Base URL for the Annolux API',
		},
	];

	authenticate: IAuthenticateGeneric = {
		type: 'generic',
		properties: {
			headers: {
				Authorization: '=Bearer {{$credentials.apiKey}}',
			},
		},
	};

	test: ICredentialTestRequest = {
		request: {
			baseURL: '={{$credentials.baseUrl || "https://api.annolux.com"}}',
			url: '/v1/search',
			method: 'POST',
			body: {
				query: 'health check',
				limit: 1,
			},
		},
	};
}
