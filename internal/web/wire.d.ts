// wire.d.ts: the Go and page contract, generated. Do not edit.
//
// Written by internal/serve/wire_test.go from the json tags of the API's types
// and every type they reach. Regenerate with `go test ./internal/serve -run WireType -update`.

export interface ProjectList {
  cursor: string;
  home: string;
  projects: ProjectRow[] | null;
}

export interface ProjectView {
  id: number;
  title: string;
  owner: string;
  owner_label: string;
  owner_present: boolean;
  working: string;
  on_you: number;
  on_agent: number;
  cursor: string;
  thread: number;
  jobs: Job[] | null;
  groups: Group[] | null;
  cards: Card[] | null;
  messages: Message[] | null;
  drafts: DraftView[] | null;
}

export interface Project {
  id: number;
  title: string;
  working: string;
  owner: string;
  thread: number;
  created_at: string;
}

export interface DraftBody {
  thread: number;
  text: string;
  answer: Answer | null;
}

export interface ProjectBody {
  title: string;
  groups: Group[] | null;
}

export interface SendResult {
  sent: number;
}

export interface ScanResult {
  folder: string;
  repos: ScannedRepo[] | null;
}

export interface AgentStatus {
  projects: Standing[] | null;
  unsettled: UnsettledSend[] | null;
}

export interface ProjectRow {
  id: number;
  title: string;
  owner: string;
  owner_label: string;
  owner_present: boolean;
  working: string;
  on_you: number;
  on_agent: number;
  groups: number;
  jobs: JobStanding[] | null;
}

export interface Job {
  id: string;
  label: string;
  stages: Stage[] | null;
}

export interface Group {
  kind: string;
  key: string;
  label: string;
  added_by: string;
}

export interface Card {
  job: string;
  group: string;
  stage: string;
  id: string;
  title: string;
  class: string;
  basis: string;
  evidence: Evidence[] | null;
  decided: string;
  recommendation: string;
  answers: Answers | null;
  revision: string;
  posted_at: string;
  thread: number;
  needs_you: boolean;
}

export interface Message {
  id: number;
  thread: string;
  who: string;
  text: string;
  when: string;
  state?: MessageState;
  reply_to?: number;
  line?: string;
}

export interface DraftView {
  id: number;
  thread: number;
  text: string;
  answer: Answer | null;
}

export interface Answer {
  kind: string;
  choice?: string;
  rows?: Record<string, string>;
  order?: string[];
  fields?: Record<string, string>;
}

export interface ScannedRepo {
  path: string;
  name: string;
  has_record: boolean;
}

export interface Standing {
  id: number;
  title: string;
  owner: string;
  owner_label: string;
  owner_present: boolean;
  working: string;
  on_you: number;
  on_agent: number;
}

export interface UnsettledSend {
  id: number;
  project: number;
  with: string;
  held: boolean;
  at: string;
  messages: UnsettledMessage[] | null;
}

export interface JobStanding {
  id: string;
  label: string;
  stage: string;
  count: string;
}

export interface Stage {
  id: string;
  label: string;
  state: string;
  count: string;
}

export interface Evidence {
  kind: string;
  ref: string;
  text: string;
  url?: string;
}

export interface Answers {
  kind: string;
  options?: Option[];
  rows?: AnswerRow[];
  fields?: Field[];
}

export type MessageState =
  | 'sent'
  | 'with_agent'
  | 'working'
  | 'answered'
  | 'declined'
  | 'failed'
  | 'no_reply';

export interface UnsettledMessage {
  id: number;
  state: MessageState;
  text: string;
  line: string;
}

export interface Option {
  id: string;
  label: string;
  detail?: string;
}

export interface AnswerRow {
  id: string;
  label: string;
  options?: Option[];
  chosen?: string;
}

export interface Field {
  id: string;
  label: string;
  type?: string;
  value?: string;
}
