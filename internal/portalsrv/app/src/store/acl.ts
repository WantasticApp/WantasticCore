import { writable, derived } from "svelte/store";
import { wsStore } from "./websocket";

export type ACLAction = "allow" | "deny";
export type ACLDirection = "ingress" | "egress";

export interface ACLRule {
  id: string;
  account_id: string;
  peer_id?: string;
  name: string;
  action: ACLAction;
  direction: ACLDirection;
  protocol?: string;
  source_ip?: string;
  dest_ip?: string;
  source_port?: number;
  dest_port?: number;
  priority: number;
  enabled: boolean;
  created_at: number;
}

export interface ACLState {
  rules: ACLRule[];
  isLoading: boolean;
  error: string | null;
}

const initialState: ACLState = {
  rules: [],
  isLoading: false,
  error: null,
};

function createACLStore() {
  const { subscribe, set, update } = writable<ACLState>(initialState);

  async function listRules(accountId?: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{ rules: ACLRule[] }>(
        "ACLService",
        "ListRules",
        {
          account_id: accountId || "",
        }
      );
      update((s) => ({ ...s, rules: response.rules || [], isLoading: false }));
      return response.rules || [];
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function getRule(ruleId: string) {
    try {
      const response = await wsStore.callGRPC<{ rule: ACLRule }>(
        "ACLService",
        "GetRule",
        { rule_id: ruleId }
      );
      return response.rule;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message }));
      throw err;
    }
  }

  async function createRule(rule: Omit<ACLRule, "id" | "created_at">) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{ rule: ACLRule }>(
        "ACLService",
        "CreateRule",
        rule
      );
      update((s) => ({
        ...s,
        rules: [...s.rules, response.rule],
        isLoading: false,
      }));
      return response.rule;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function updateRule(ruleId: string, updates: Partial<ACLRule>) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      const response = await wsStore.callGRPC<{ rule: ACLRule }>(
        "ACLService",
        "UpdateRule",
        {
          rule_id: ruleId,
          ...updates,
        }
      );
      update((s) => ({
        ...s,
        rules: s.rules.map((r) => (r.id === ruleId ? response.rule : r)),
        isLoading: false,
      }));
      return response.rule;
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  async function deleteRule(ruleId: string) {
    update((s) => ({ ...s, isLoading: true, error: null }));
    try {
      await wsStore.callGRPC<{ success: boolean }>("ACLService", "DeleteRule", {
        rule_id: ruleId,
      });
      update((s) => ({
        ...s,
        rules: s.rules.filter((r) => r.id !== ruleId),
        isLoading: false,
      }));
    } catch (err: any) {
      update((s) => ({ ...s, error: err.message, isLoading: false }));
      throw err;
    }
  }

  const rules = derived({ subscribe }, (s) => s.rules);
  const isLoading = derived({ subscribe }, (s) => s.isLoading);

  return {
    subscribe,
    listRules,
    getRule,
    createRule,
    updateRule,
    deleteRule,
    rules,
    isLoading,
  };
}

export const aclStore = createACLStore();
