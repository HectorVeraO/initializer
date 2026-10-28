export interface KratosMessage {
  readonly id: number;
  readonly text: string;
  readonly type: 'error' | 'info' | 'success';
}

export interface KratosNode {
  readonly attributes: { readonly name?: string; readonly value?: unknown; readonly type?: string };
  readonly messages: readonly KratosMessage[];
}

/** Subset of the Kratos login flow the console reads. */
export interface KratosLoginFlow {
  readonly id: string;
  readonly ui: { readonly action: string; readonly method: string; readonly nodes: readonly KratosNode[]; readonly messages?: readonly KratosMessage[] };
}

export type KratosLoginResult = { readonly kind: 'signed-in' } | { readonly kind: 'rejected'; readonly messages: readonly KratosMessage[] } | { readonly kind: 'unavailable' };
