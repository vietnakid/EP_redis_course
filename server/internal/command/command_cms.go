// command_cms.go handles the Count-Min Sketch keyspace: CMS.INITBYDIM,
// CMS.INITBYPROB, CMS.INCRBY, CMS.QUERY, CMS.INFO. Each key routes
// through datastructure.CMSStore. CMS.MERGE is out of scope.
//
// Note the deliberate asymmetry with BF.MADD: a sketch is never created
// implicitly. Bloom's defaults are a reasonable guess, but a sketch's
// width and depth encode *your* error budget - guessing them would hand
// back estimates whose accuracy nobody chose.
package command

import (
	"strconv"

	"redis_k2/server/internal/datastructure"
	"redis_k2/server/internal/protocol"
)

// initCMS is the shared tail of INITBYDIM and INITBYPROB: refuse an
// existing key, otherwise store a fresh sketch of the given dimensions.
func initCMS(key string, width uint32, depth uint32) []byte {
	if reply := checkType(key, datastructure.KindCMS); reply != nil {
		return reply
	}
	if _, exists := datastructure.CMSStore[key]; exists {
		return protocol.EncodeError("CMS: key already exists")
	}
	datastructure.CMSStore[key] = datastructure.CreateCMS(width, depth)
	return protocol.EncodeSimpleString("OK")
}

// handleCMSInitByDim implements CMS.INITBYDIM key width depth: dimensions
// stated directly, for when you already know the memory you want to spend.
func handleCMSInitByDim(args []string) []byte {
	if len(args) != 3 {
		return protocol.EncodeError("CMS: wrong number of arguments for 'cms.initbydim' command")
	}
	width, err := strconv.ParseUint(args[1], 10, 32)
	if err != nil || width == 0 {
		return protocol.EncodeError("CMS: invalid width")
	}
	depth, err := strconv.ParseUint(args[2], 10, 32)
	if err != nil || depth == 0 {
		return protocol.EncodeError("CMS: invalid depth")
	}
	return initCMS(args[0], uint32(width), uint32(depth))
}

// handleCMSInitByProb implements CMS.INITBYPROB key error probability:
// state the error budget and let CalcCMSDim derive width and depth.
func handleCMSInitByProb(args []string) []byte {
	if len(args) != 3 {
		return protocol.EncodeError("CMS: wrong number of arguments for 'cms.initbyprob' command")
	}
	errRate, err := strconv.ParseFloat(args[1], 64)
	if err != nil || errRate <= 0 || errRate >= 1 {
		return protocol.EncodeError("CMS: invalid overestimation value")
	}
	errProb, err := strconv.ParseFloat(args[2], 64)
	if err != nil || errProb <= 0 || errProb >= 1 {
		return protocol.EncodeError("CMS: invalid prob value")
	}
	width, depth := datastructure.CalcCMSDim(errRate, errProb)
	return initCMS(args[0], width, depth)
}

// handleCMSIncrBy implements CMS.INCRBY key item incr [item incr ...],
// replying with one new estimate per item. Every pair is parsed before
// anything is incremented, so a bad increment halfway through doesn't
// leave the sketch half-updated - and a sketch can't be un-incremented.
func handleCMSIncrBy(args []string) []byte {
	if len(args) < 3 || len(args)%2 != 1 {
		return protocol.EncodeError("CMS: wrong number of arguments for 'cms.incrby' command")
	}
	key := args[0]
	if reply := checkType(key, datastructure.KindCMS); reply != nil {
		return reply
	}

	type pair struct {
		item string
		incr uint32
	}
	pairs := make([]pair, 0, (len(args)-1)/2)
	for i := 1; i < len(args); i += 2 {
		incr, err := strconv.ParseUint(args[i+1], 10, 32)
		if err != nil {
			return protocol.EncodeError("CMS: invalid increment")
		}
		pairs = append(pairs, pair{item: args[i], incr: uint32(incr)})
	}

	cms, exists := datastructure.CMSStore[key]
	if !exists {
		return protocol.EncodeError("CMS: key does not exist")
	}
	replies := make([]protocol.Value, 0, len(pairs))
	for _, p := range pairs {
		replies = append(replies, int64(cms.IncrBy(p.item, p.incr)))
	}
	return protocol.Encode(replies)
}

// handleCMSQuery implements CMS.QUERY key item [item ...]: the estimated
// count of each item, in the order asked.
func handleCMSQuery(args []string) []byte {
	if len(args) < 2 {
		return protocol.EncodeError("CMS: wrong number of arguments for 'cms.query' command")
	}
	key := args[0]
	if reply := checkType(key, datastructure.KindCMS); reply != nil {
		return reply
	}
	cms, exists := datastructure.CMSStore[key]
	if !exists {
		return protocol.EncodeError("CMS: key does not exist")
	}
	replies := make([]protocol.Value, 0, len(args)-1)
	for _, item := range args[1:] {
		replies = append(replies, int64(cms.Count(item)))
	}
	return protocol.Encode(replies)
}

// handleCMSInfo implements CMS.INFO key, replying with the flat
// field/value array real Redis uses: width W depth D count C. count is
// the total of every increment applied, i.e. the size of the stream the
// sketch has seen.
func handleCMSInfo(args []string) []byte {
	if len(args) != 1 {
		return protocol.EncodeError("CMS: wrong number of arguments for 'cms.info' command")
	}
	key := args[0]
	if reply := checkType(key, datastructure.KindCMS); reply != nil {
		return reply
	}
	cms, exists := datastructure.CMSStore[key]
	if !exists {
		return protocol.EncodeError("CMS: key does not exist")
	}
	width, depth, count := cms.Info()
	return protocol.Encode([]protocol.Value{
		"width", int64(width),
		"depth", int64(depth),
		"count", int64(count),
	})
}
