/* Page-global exposure for bundle 2 (v0.58): only lpEntries --
   the node boot suites read it as a bare global after boot. */
import { lpEntries } from './launchpad';
Object.defineProperty(window, 'lpEntries', { configurable: true, enumerable: true, get: () => lpEntries });
