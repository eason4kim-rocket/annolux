import type { IExecuteFunctions, INodeExecutionData, INodeType, INodeTypeDescription, ISupplyDataFunctions, SupplyData } from 'n8n-workflow';
export declare class Annolux implements INodeType {
    description: INodeTypeDescription;
    execute(this: IExecuteFunctions): Promise<INodeExecutionData[][]>;
    supplyData(this: ISupplyDataFunctions, itemIndex: number): Promise<SupplyData>;
}
//# sourceMappingURL=Annolux.node.d.ts.map