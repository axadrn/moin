package service

import (
	"math/rand/v2"
	"strings"
)

// A server added without a name gets one like brave-otter: an adjective
// and a noun, short, friendly and easy to type.
var (
	nameAdjectives = strings.Fields(`
	able amber ample apt arctic azure balmy basic beige bold bouncy brave breezy
	brief bright brisk bubbly busy calm candid casual chalky cheery chill
	chipper chirpy civil clean clear clever close cloudy cocoa comfy cool coral
	cosmic cozy crafty crisp cuddly curly curvy cute daily dainty dandy dapper
	daring dashing deep deft dewy dizzy dreamy dusty eager early earthy easy
	elated elfin epic even exact fair fancy fast feisty fern fiery fine firm fit
	fizzy fleet fluffy foggy frank free fresh frisky frosty fuzzy gentle giddy
	glad gleamy glossy golden grand grassy great green handy happy hardy hasty
	hazy hearty helpful honest humble icy ideal inky jaunty jazzy jolly jumpy
	keen kind lavish lazy leafy light lilac limber lively lofty loyal lucid
	lucky lunar mellow merry mighty milky minty misty modest mossy neat nifty
	nimble noble nutty oaken olive open orange peachy peppy perky plucky plush
	polite proud quick quiet quirky rapid ready regal rosy round royal ruby
	rustic rusty sandy sassy savvy shiny silky silver simple sleek sleepy slick
	smart smooth snappy snowy snug soft solar sonic sparky speedy spicy spry
	steady stellar sterling stormy sturdy sublime sunny super sweet swift tame
	tangy tawny tender tidy tiny toasty topaz tranquil trim trusty tulip upbeat
	urban valid velvet vivid wacky warm wavy windy wise witty woolly zany zappy
	zesty zippy
	`)

	nameNouns = strings.Fields(`
	acorn alpaca anchor apple apricot aspen badger bagel bamboo banjo basil
	beacon beagle beaver berry birch biscuit bison blossom bluebird bobcat bongo
	brook bubble buffalo bumble bunny butter button cactus camel canary candle
	canyon caramel cashew cedar cherry chestnut chipmunk cider clover cobalt
	cocoa comet cookie coral cosmos cougar coyote crane crayon cricket crocus
	crumble crumpet cupcake daisy dingo dolphin donut dove dragon drum dune
	eagle ember falcon fennel ferret fig finch firefly fjord flamingo fox frog
	gazelle gecko geyser ginger glacier goose gopher grape gull harbor hazel
	hedgehog heron hippo honey hummus husky iguana island jackal jaguar jasmine
	jelly kayak kestrel kiwi koala lagoon lantern lark lemon lemur lily lime
	llama lobster lotus lynx mango maple marble meadow melon meteor mint mole
	moose mosaic muffin narwhal nectar newt noodle nutmeg oak oasis ocelot olive
	orbit orca otter owl panda papaya parrot peach peanut pebble pecan pelican
	penguin pepper pickle pigeon pine plum pony poppy prairie pretzel puffin
	pumpkin quail quartz quince rabbit raccoon radish raven reef ripple robin
	rocket sage salmon sapling sequoia shrimp sloth sparrow spruce squid
	squirrel starling stork summit swan taco thistle thrush tiger toucan trout
	tulip tundra turnip turtle valley violet walnut walrus waffle wallaby willow
	wombat wren yak zebra zephyr
	`)
)

// freeName is a random adjective-noun no server of the workspace has. After
// a few collisions it adds a second noun, which leaves plenty of room.
func (s *ServerService) freeName(workspaceID string) string {
	taken := map[string]bool{}
	servers, err := s.servers.ListByWorkspaceID(workspaceID)
	if err == nil {
		for _, server := range servers {
			taken[server.Name] = true
		}
	}
	for attempt := 0; ; attempt++ {
		words := []string{pick(nameAdjectives), pick(nameNouns)}
		if attempt >= 10 {
			words = append(words, pick(nameNouns))
		}
		name := strings.Join(words, "-")
		if !taken[name] {
			return name
		}
	}
}

func pick(words []string) string { return words[rand.IntN(len(words))] }
