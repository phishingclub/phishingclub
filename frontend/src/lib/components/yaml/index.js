// ES module wrapper for js-yaml UMD bundle
import './js-yaml.js';

// the UMD bundle assigns to globalThis.jsyaml
const jsyaml = globalThis.jsyaml;

export const load = jsyaml.load;
export const dump = jsyaml.dump;
export const loadAll = jsyaml.loadAll;
export const Schema = jsyaml.Schema;
export const Type = jsyaml.Type;
export const YAMLException = jsyaml.YAMLException;
export const CORE_SCHEMA = jsyaml.CORE_SCHEMA;
export const DEFAULT_SCHEMA = jsyaml.DEFAULT_SCHEMA;
export const FAILSAFE_SCHEMA = jsyaml.FAILSAFE_SCHEMA;
export const JSON_SCHEMA = jsyaml.JSON_SCHEMA;

export default jsyaml;
