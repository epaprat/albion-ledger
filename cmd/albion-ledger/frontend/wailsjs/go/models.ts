export namespace model {
	
	export class CaptureStatusView {
	    capturing: boolean;
	    interface: string;
	    gameServer?: string;
	    encryptedRate: number;
	    driftAlert?: string;
	
	    static createFrom(source: any = {}) {
	        return new CaptureStatusView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.capturing = source["capturing"];
	        this.interface = source["interface"];
	        this.gameServer = source["gameServer"];
	        this.encryptedRate = source["encryptedRate"];
	        this.driftAlert = source["driftAlert"];
	    }
	}
	export class Item {
	    index: number;
	    displayName: string;
	    uniqueName?: string;
	    tier: number;
	    enchant: number;
	    quality: number;
	    known: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Item(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.displayName = source["displayName"];
	        this.uniqueName = source["uniqueName"];
	        this.tier = source["tier"];
	        this.enchant = source["enchant"];
	        this.quality = source["quality"];
	        this.known = source["known"];
	    }
	}
	export class Valuation {
	    amount: number;
	    source: string;
	    asOf: number;
	    stale: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Valuation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.amount = source["amount"];
	        this.source = source["source"];
	        this.asOf = source["asOf"];
	        this.stale = source["stale"];
	    }
	}
	export class LiveViewItem {
	    item: Item;
	    valuation: Valuation;
	    lastSeen: number;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new LiveViewItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.item = this.convertValues(source["item"], Item);
	        this.valuation = this.convertValues(source["valuation"], Valuation);
	        this.lastSeen = source["lastSeen"];
	        this.count = source["count"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

