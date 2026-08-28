export interface AdminHandoff {
  id: string;
  ticket_number: number;
  agent_id: string;
  source_channel: string;
  source_chat_id: string;
  priority: "normal" | "high" | "urgent";
  service: string;
  identifiers: string[];
  summary: string;
  status: "pending" | "completed" | "delivery_failed" | "dismissed";
  created_at: string;
  completed_at?: string;
  completion_message?: string;
}

export interface AdminHandoffEvent {
  id: string;
  action: string;
  actor_type: string;
  actor_id: string;
  content?: string;
  created_at: string;
}

export const ticketReference = (ticket: number) => `Ticket-${String(ticket).padStart(6, "0")}`;
