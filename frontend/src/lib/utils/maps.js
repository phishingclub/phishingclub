export class BiMap {
    #keys = [];
    #values = [];
    /**
     * BiMap is a two-way map that allows you to look up values by key, and keys by value. 
     * Can only accept maps with unique values.
     * 
     * @param {{[key: string]: string}} map 
     */
    constructor(map) {
        // Check for duplicate values
        const values = Object.values(map);
        if (new Set(values).size !== values.length) {
            console.error('BiMap has received a map with duplicate values. This is not allowed.', values);
        }
        const keys = Object.keys(map);
        if (new Set(keys).size !== keys.length) {
            console.error('BiMap has received a map with duplicate keys. This is not allowed.', keys);
        }
        this.#keys = Object.keys(map);
        this.#values = Object.values(map);
    }

    /**
     * Transform an array of objects into a BiMap
     * Ex. [{key: 'a', value: 'b'}, {key: 'c', value: 'd'}] => BiMap {a: 'b', c: 'd'}
     * Default to key = id and value = name
     * @param {Object[]} arr 
     * @param {string} keyName 
     * @param {string} valueName 
     * @returns 
     */
    static FromArrayOfObjects(arr, keyName = 'id', valueName = 'name') {
        /** 
         * @type {{[key: string]: string}} arr
         **/
        const map = {};
        arr.forEach((obj) => {
            map[obj[keyName]] = obj[valueName];
        });
        return new BiMap(map);

    }

    /**
     * @returns {string[]}
     */
    keys() {
        return this.#keys;
    }

    /**
     * @returns {string[]}
     */
    values() {
        return this.#values;
    }

    /**
     * Get value by key
     * Returns an empty string if the key is not found
     *  
     * @param {string} key 
     * @returns {any}
     */
    byKey(key) {
        const v = this.#values[this.#keys.indexOf(key)];
        if (v === undefined) {
            return "";
        }
        return v
    }

    /**
     * Get key by value
     * Returns an empty string if the value is not found
     *  
     * @param {string} value
     * @returns {string}
     */
    byValue(value) {
        const v = this.#keys[this.#values.indexOf(value)];
        if (v === undefined) {
            return "";
        }
        return v
    }

    /**
     * Get value by key or null
     *  
     * @param {string} value
     * @param {*} value
     * @returns {*}
     */
    byValueOrNull(value) {
        const v = this.#keys[this.#values.indexOf(value)];
        if(!v) {
            return null;
        }
        return v;
    }
}
