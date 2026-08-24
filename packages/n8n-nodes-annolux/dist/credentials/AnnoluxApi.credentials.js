"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.AnnoluxApi = void 0;
class AnnoluxApi {
    constructor() {
        this.name = 'annoluxApi';
        this.displayName = 'Annolux API';
        this.documentationUrl = 'https://annolux.com/docs';
        this.properties = [
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
        this.authenticate = {
            type: 'generic',
            properties: {
                headers: {
                    Authorization: '=Bearer {{$credentials.apiKey}}',
                },
            },
        };
        this.test = {
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
}
exports.AnnoluxApi = AnnoluxApi;
//# sourceMappingURL=AnnoluxApi.credentials.js.map