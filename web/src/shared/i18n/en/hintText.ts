import type { hintText as ru } from '../ru/hintText.ts'

// Раздел каталога, который грузится по первому нажатию «?» (см. index.ts
// и shared/ui/Hint.tsx): пояснения нужны, только когда их просят.
export const hintText: typeof ru = {
  markup: 'Two marks on the columns: where work starts and where it ends. Cycle time, throughput and the forecast are counted from them, not from dates typed in by hand.',
  limit: 'How many cards the column should hold at once. Over the limit the counter turns amber, but moving in is still allowed: the limit makes overload visible. A hard limit refuses a move into a full column.',
  promise: 'How many days finished work usually takes to cross the board. A card running longer is marked on the board, and the “Longer than promised” filter gathers such cards.',
  block: 'The card is waiting for something, and the reason is visible right on the board. If you know when the wait ends, set a deadline — the block will lift itself.',
  labelScope: 'A label belongs to the organisation, to a subdivision with everything inside it, or to one board, and is offered only there. The same name cannot be used twice where scopes overlap.',
  visibility: 'Who sees the board: the whole organisation, its own subdivision, or listed people only. Nobody else sees it anywhere.',
  iteration: 'A sprint or a release: a name and dates that cards are attached to. Closing it freezes what it contains for good and produces a report.',
  cycleTime: 'How many days work takes from the column where it starts to the column where it ends. Shown as percentiles: an average would hide the long tail.',
  age: 'How many days each started card has been going. Age speaks not about the past but about what is stuck right now.',
  accumulation: 'Three bands by day: not started, in progress, done. An in-progress band that keeps growing means more is started than finished.',
  throughput: 'How many cards a week reach the column where work ends. Cards taken off the board unfinished do not count.',
  forecast: 'How many days finishing so many cards may take. It samples random weeks from the past and says one thing: what happens if it goes on as it went.',
  roles: 'An owner reads, changes work and runs the organisation. A member reads and changes work. A viewer only reads.',
  subdivisionAdmin: 'Runs their own subtree: who is in it, nested subdivisions and their boards. Appointed by the organisation owner.',
  keyScopes: 'What another system may do with this key: read boards, change them, read the structure or the log. Grant what the integration needs: a leaked key can do no more.',
  subscription: 'An address in your system where Takt sends an event every time something happens to cards. If one does not arrive, Takt retries, and after a long run of failures switches the subscription off.',
  export: 'All the organisation’s data in one file — to take it home or to move. The audit log is optional: it is usually larger than everything else.',
  savedView: 'A filter, a grouping and a layout under a name. All of it lives in the page address, so a view can be opened again or sent as a link.',
  grouping: 'The board sliced into rows by assignee, label, iteration, priority or the work tree — parent or root. A slice of the same board: to move a card to another lane, change its property.',
}
