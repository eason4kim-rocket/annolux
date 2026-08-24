"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.Annolux = void 0;
class Annolux {
    constructor() {
        this.description = {
            displayName: 'Annolux Search',
            name: 'annolux',
            icon: 'file:annolux.svg',
            group: ['transform'],
            version: 1,
            subtitle: '={{$parameter["operation"] + ": " + $parameter["query"]}}',
            description: 'Search curated English and Chinese technical corpus with explicit snapshot timestamps for AI Agents',
            defaults: {
                name: 'Annolux Search',
            },
            inputs: ['main'],
            outputs: ['main'],
            credentials: [
                {
                    name: 'annoluxApi',
                    required: true,
                },
            ],
            properties: [
                {
                    displayName: 'Operation',
                    name: 'operation',
                    type: 'options',
                    noDataExpression: true,
                    options: [
                        {
                            name: 'Search Web',
                            value: 'search',
                            description: 'Search curated English & Chinese web corpus with explicit fetched_at time provenance',
                            action: 'Search curated web corpus',
                        },
                    ],
                    default: 'search',
                },
                {
                    displayName: 'Query',
                    name: 'query',
                    type: 'string',
                    default: '',
                    placeholder: 'e.g. latest Next.js server actions best practices',
                    required: true,
                    description: 'Natural language search query (up to 50 words)',
                },
                {
                    displayName: 'Limit',
                    name: 'limit',
                    type: 'number',
                    typeOptions: {
                        minValue: 1,
                        maxValue: 10,
                    },
                    default: 10,
                    description: 'Maximum number of results to return (1 to 10)',
                },
                {
                    displayName: 'Domain Filters',
                    name: 'domains',
                    type: 'string',
                    default: '',
                    placeholder: 'e.g. docs.rs, go.dev, github.com',
                    description: 'Optional comma-separated list of registrable domains to restrict results to',
                },
                {
                    displayName: 'Deduplicate',
                    name: 'deduplicate',
                    type: 'boolean',
                    default: true,
                    description: 'Whether to collapse duplicate and near-duplicate results',
                },
                {
                    displayName: 'Ranking',
                    name: 'ranking',
                    type: 'options',
                    options: [
                        {
                            name: 'Default (Recency + Precision)',
                            value: 'default',
                        },
                        {
                            name: 'Provider (Lexical Anchor)',
                            value: 'provider',
                        },
                    ],
                    default: 'default',
                    description: 'Ranking algorithm to prioritize',
                },
            ],
        };
    }
    async execute() {
        const items = this.getInputData();
        const returnData = [];
        const credentials = await this.getCredentials('annoluxApi');
        const baseUrl = credentials.baseUrl || 'https://api.annolux.com';
        for (let i = 0; i < items.length; i++) {
            try {
                const query = this.getNodeParameter('query', i);
                const limit = this.getNodeParameter('limit', i, 10);
                const domainsStr = this.getNodeParameter('domains', i, '');
                const deduplicate = this.getNodeParameter('deduplicate', i, true);
                const ranking = this.getNodeParameter('ranking', i, 'default');
                const payload = {
                    query,
                    limit,
                    deduplicate,
                    ranking,
                };
                if (domainsStr && domainsStr.trim()) {
                    payload.domains = domainsStr
                        .split(',')
                        .map((d) => d.trim())
                        .filter(Boolean);
                }
                const options = {
                    method: 'POST',
                    baseURL: baseUrl,
                    url: '/v1/search',
                    body: payload,
                    json: true,
                    headers: {
                        Authorization: `Bearer ${credentials.apiKey}`,
                    },
                };
                const response = await this.helpers.httpRequest(options);
                returnData.push({
                    json: response,
                    pairedItem: { item: i },
                });
            }
            catch (error) {
                if (this.continueOnFail()) {
                    returnData.push({
                        json: { error: error.message },
                        pairedItem: { item: i },
                    });
                    continue;
                }
                throw error;
            }
        }
        return [returnData];
    }
    async supplyData(itemIndex) {
        const credentials = await this.getCredentials('annoluxApi');
        const baseUrl = credentials.baseUrl || 'https://api.annolux.com';
        return {
            response: {
                name: 'search_web',
                description: 'Search curated English and Chinese technical & knowledge corpus with explicit snapshot timestamps and zero hallucination.',
                parameters: {
                    type: 'object',
                    properties: {
                        query: {
                            type: 'string',
                            description: 'The search query to look up on the web.',
                        },
                        limit: {
                            type: 'number',
                            description: 'Number of results to return (1-10, default 10).',
                        },
                        domains: {
                            type: 'array',
                            items: { type: 'string' },
                            description: 'Optional list of domain filters without protocol (e.g. ["docs.rs"]).',
                        },
                    },
                    required: ['query'],
                },
                func: async (args) => {
                    const options = {
                        method: 'POST',
                        baseURL: baseUrl,
                        url: '/v1/search',
                        body: {
                            query: args.query,
                            limit: args.limit || 10,
                            domains: args.domains,
                            deduplicate: true,
                            ranking: 'default',
                        },
                        json: true,
                        headers: {
                            Authorization: `Bearer ${credentials.apiKey}`,
                        },
                    };
                    const response = await this.helpers.httpRequest(options);
                    return JSON.stringify(response);
                },
            },
        };
    }
}
exports.Annolux = Annolux;
//# sourceMappingURL=Annolux.node.js.map