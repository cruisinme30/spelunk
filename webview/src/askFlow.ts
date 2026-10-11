// Ask, from the panel's side: a question in the box goes to the host, the
// query it becomes lands in the box and runs, and the line under the box
// says what was asked, or what Ask needs first.
import { send } from "./host";
import type { Layout } from "./layout";
import type { AskResultMessage } from "./protocol.gen";
import { questionIn, renderAsked } from "./render/asked";
import type { ViewState } from "./state";

/** What the flow needs from the panel: the box, and the two ways of putting text in it. */
export interface AskFlowDeps {
  layout: Layout;
  state: ViewState;
  /** Puts text in the box and searches it at once. */
  setQuery: (text: string) => void;
}

/** The panel's side of Ask. */
export class AskFlow {
  /** A flow over `deps`: the panel's box, state and ways of filling the box. */
  constructor(private readonly deps: AskFlowDeps) {}

  /** Asks the box's question in plain words: its text, with or without a final "?". */
  askBox(): void {
    const { input } = this.deps.layout;
    const question = questionIn(input.value) ?? input.value.trim();
    if (question) this.ask(question);
  }

  /** Whether the box holds a question Enter should ask: it ends in "?" and Ask is on. */
  boxIsQuestion(): boolean {
    return this.deps.state.ui.askEnabled && questionIn(this.deps.layout.input.value) !== undefined;
  }

  /**
   * Asks `question`: the host turns it into a query, which then runs.
   * `broaden` is the query on screen that found nothing: a looser one is
   * asked for. Ask off in settings says so instead of asking.
   */
  ask(question: string, broaden?: string): void {
    const { state } = this.deps;
    const seq = (state.asked?.seq ?? 0) + 1;
    if (!state.ui.askEnabled) {
      state.asked = { seq, question, pending: false, assumptions: [], setup: "off" };
      this.render();
      return;
    }
    state.asked = { seq, question, pending: true, assumptions: [] };
    this.render();
    send("ask", { question, seq, ...(broaden === undefined ? {} : { broaden }) });
  }

  /** Broaden: asks for a looser query than the one on screen, which found nothing. */
  broaden(): void {
    const asked = this.deps.state.asked;
    if (asked?.query !== undefined) this.ask(asked.question, asked.query);
  }

  /** The answer to the newest ask: the query goes in the box and runs, or the line says what Ask needs. */
  onResult(result: AskResultMessage): void {
    const { state } = this.deps;
    const seq = state.asked?.seq;
    if (seq === undefined || seq !== result.seq) return;
    state.asked = {
      seq,
      question: result.question,
      pending: false,
      assumptions: result.assumptions ?? [],
      ...(result.query === undefined ? {} : { query: result.query }),
      ...(result.explanation === undefined ? {} : { explanation: result.explanation }),
      ...(result.setup === undefined ? {} : { setup: result.setup }),
      ...(result.clarify === undefined ? {} : { clarify: result.clarify }),
      ...(result.error === undefined ? {} : { error: result.error }),
    };
    this.render();
    if (result.query !== undefined) this.deps.setQuery(result.query);
  }

  /** Typing over the query a question became ends the question. */
  onTyped(): void {
    if (this.deps.state.asked?.pending === false) this.clear();
  }

  /** Forgets the question, and clears its line. */
  clear(): void {
    this.deps.state.asked = undefined;
    this.render();
  }

  private render(): void {
    const { layout, state } = this.deps;
    renderAsked(layout, state, {
      // The question goes back in the box as if typed, so the host, the chips and a reopened panel all hold it.
      onEditQuestion: () => {
        const question = state.asked?.question ?? "";
        this.clear();
        this.deps.setQuery(`${question}?`);
        layout.input.focus();
      },
      onAskAgain: () => {
        const question = state.asked?.question;
        if (question) this.ask(question);
      },
      onSearchWords: () => {
        const question = state.asked?.question ?? "";
        this.clear();
        this.deps.setQuery(`${question}?`);
      },
      onSetup: (action) => {
        send("ask.setup", { action });
      },
    });
  }
}
